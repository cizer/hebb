package core

import (
	"database/sql"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
)

// noteGraph is an undirected adjacency representation of the vault link graph.
// Nodes are all notes in the database; edges are resolved links (target_path IS
// NOT NULL). Dangling links (NULL target_path) contribute no edges. Self-loops
// are dropped. Edges are deduplicated so a pair (A,B) appears once regardless
// of how many directed links exist between the two notes.
type noteGraph struct {
	// nodes is the ordered slice of all note paths (sorted for determinism).
	nodes []string
	// nodeIdx maps each note path to its integer index in nodes.
	nodeIdx map[string]int
	// adj is the adjacency list: adj[i] is the sorted set of neighbour indices
	// of node i. Sorted for determinism; no self-loops; no duplicates.
	adj [][]int
}

// degree returns the number of distinct neighbours of node i.
func (g *noteGraph) degree(i int) int { return len(g.adj[i]) }

// nodeCount returns the total number of nodes (notes).
func (g *noteGraph) nodeCount() int { return len(g.nodes) }

// edgeCount returns the number of undirected edges.
func (g *noteGraph) edgeCount() int {
	total := 0
	for _, nb := range g.adj {
		total += len(nb)
	}
	return total / 2
}

// validateExcludePatterns checks every glob pattern for validity up front, so a
// malformed pattern (e.g. an unclosed "[") is reported rather than silently
// treated as a non-match. The whole point of the feature is graph-metric
// fidelity, so silently computing the wrong graph from a typo would invalidate
// the result. path.Match reports a bad pattern via ErrBadPattern regardless of
// the candidate string, so an empty candidate is enough to validate. An empty
// patterns slice validates trivially.
func validateExcludePatterns(patterns []string) error {
	for _, pat := range patterns {
		if _, err := path.Match(pat, ""); err != nil {
			return fmt.Errorf("invalid exclude_from_graph pattern %q: %w", pat, err)
		}
	}
	return nil
}

// matchesExcludePatterns reports whether a note should be excluded from the
// graph. A note is excluded when any of the supplied glob patterns matches any
// of the three candidates: title, basename-without-.md, or vault-relative path.
// Matching uses path.Match semantics (shell-style globs over the '/'-separated
// vault path, OS-independent: "*" does not cross "/"). Patterns are assumed
// pre-validated by validateExcludePatterns, so a match error here cannot occur.
// An empty patterns slice always returns false (exclude nothing).
func matchesExcludePatterns(patterns []string, title, notePath string) bool {
	if len(patterns) == 0 {
		return false
	}
	base := strings.TrimSuffix(path.Base(notePath), ".md")
	for _, pat := range patterns {
		if ok, _ := path.Match(pat, title); ok {
			return true
		}
		if ok, _ := path.Match(pat, base); ok {
			return true
		}
		if ok, _ := path.Match(pat, notePath); ok {
			return true
		}
	}
	return false
}

// buildGraph reads the notes and resolved links tables and constructs the
// undirected note graph. It is called by RunHealthFull (once per invocation,
// shared across all graph-based detectors) and by GraphHealth (for stats-only
// callers such as tests and future tooling).
func buildGraph(db *sql.DB) (*noteGraph, error) {
	return buildGraphExcluding(db, nil)
}

// buildGraphExcluding is the underlying implementation for buildGraph. It
// accepts an optional slice of glob patterns (see matchesExcludePatterns); when
// non-empty, any note whose title, basename-without-.md, or vault-relative path
// matches ANY pattern is excluded from the graph entirely: it becomes neither a
// node nor the endpoint of any edge. Pass nil (or an empty slice) to build the
// full graph without exclusions.
func buildGraphExcluding(db *sql.DB, excludePatterns []string) (*noteGraph, error) {
	// Validate the patterns before touching the DB: a malformed glob must fail
	// the run with a clear message, not silently exclude nothing and report
	// metrics over the unfiltered graph.
	if err := validateExcludePatterns(excludePatterns); err != nil {
		return nil, err
	}
	// Load all note paths and titles, ordered for determinism.
	rows, err := db.Query("SELECT path, title FROM notes ORDER BY path")
	if err != nil {
		return nil, fmt.Errorf("graph: load notes: %w", err)
	}
	var nodes []string
	// excluded is the set of paths that are filtered out by excludePatterns. It
	// is used when loading edges to drop any edge incident to an excluded node.
	excluded := make(map[string]bool)
	for rows.Next() {
		var p, title string
		if err := rows.Scan(&p, &title); err != nil {
			rows.Close()
			return nil, fmt.Errorf("graph: scan note path: %w", err)
		}
		if matchesExcludePatterns(excludePatterns, title, p) {
			excluded[p] = true
			continue
		}
		nodes = append(nodes, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("graph: notes rows: %w", err)
	}

	// Build the node index.
	nodeIdx := make(map[string]int, len(nodes))
	for i, p := range nodes {
		nodeIdx[p] = i
	}

	// Initialise empty adjacency lists.
	adj := make([][]int, len(nodes))
	for i := range adj {
		adj[i] = []int{}
	}

	// Load resolved links and build an undirected, deduplicated edge set.
	// The canonical edge is keyed by (min-index, max-index) so bidirectional
	// links between the same pair of notes contribute exactly one edge.
	type edge struct{ a, b int }
	seen := make(map[edge]bool)

	lrows, err := db.Query("SELECT source_path, target_path FROM links WHERE target_path IS NOT NULL")
	if err != nil {
		return nil, fmt.Errorf("graph: load links: %w", err)
	}
	for lrows.Next() {
		var src, tgt string
		if err := lrows.Scan(&src, &tgt); err != nil {
			lrows.Close()
			return nil, fmt.Errorf("graph: scan link: %w", err)
		}
		// Drop any edge incident to an excluded note.
		if excluded[src] || excluded[tgt] {
			continue
		}
		si, sok := nodeIdx[src]
		ti, tok := nodeIdx[tgt]
		if !sok || !tok {
			// Source or target not in notes table (index inconsistency); skip.
			continue
		}
		if si == ti {
			// Self-loop; drop.
			continue
		}
		// Normalise to a < b to deduplicate (A->B) and (B->A).
		if si > ti {
			si, ti = ti, si
		}
		e := edge{si, ti}
		if seen[e] {
			continue
		}
		seen[e] = true
		adj[si] = append(adj[si], ti)
		adj[ti] = append(adj[ti], si)
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return nil, fmt.Errorf("graph: links rows: %w", err)
	}

	// Sort each adjacency list for determinism.
	for i := range adj {
		sort.Ints(adj[i])
	}

	return &noteGraph{nodes: nodes, nodeIdx: nodeIdx, adj: adj}, nil
}

// GraphStats is the structural summary returned by GraphHealth. It is consumed
// by the hebb health summary line and by the Phase 2b dashboard.
type GraphStats struct {
	// NodeCount is the total number of notes (graph nodes).
	NodeCount int `json:"node_count"`
	// EdgeCount is the number of undirected resolved links (graph edges).
	EdgeCount int `json:"edge_count"`
	// ComponentCount is the number of connected components.
	ComponentCount int `json:"component_count"`
	// GiantRatio is the fraction of notes in the largest connected component
	// (0.0 when the vault is empty).
	GiantRatio float64 `json:"giant_ratio"`
	// MaxCore is the maximum k-core coreness value across all notes.
	MaxCore int `json:"max_core"`
	// CoreCount maps coreness level to the number of notes at that level.
	CoreCount map[int]int `json:"core_count"`
	// Coreness maps each note path to its k-core coreness number.
	Coreness map[string]int `json:"coreness"`
}

// GraphHealth builds the undirected link graph over all notes and computes:
//   - connected components via union-find,
//   - k-core coreness via iterative degree-peel.
//
// It returns the full GraphStats summary. GraphHealth is used directly by the
// web layer (/api/health calls RunHealthFull, which builds its own graph
// internally) and by graph-stats tests. RunHealthFull does not call GraphHealth;
// it calls buildGraphExcluding itself and then calls computeComponents and
// computeCoreness directly so it can reuse the same graph for detectOrphansAndLeaves
// and detectIslands without a second DB round-trip. Consolidating the two paths
// would require changing GraphHealth's signature to accept a pre-built graph;
// left separate to keep the public API stable.
//
// Any notes matched by cfg.Health.GetExcludeFromGraph() are removed from the
// graph before metrics are computed (see matchesExcludePatterns).
func GraphHealth(cfg Config, db *sql.DB) (GraphStats, error) {
	g, err := buildGraphExcluding(db, cfg.Health.GetExcludeFromGraph())
	if err != nil {
		return GraphStats{}, err
	}
	if g.nodeCount() == 0 {
		return GraphStats{
			CoreCount: map[int]int{},
			Coreness:  map[string]int{},
		}, nil
	}

	compCount, giantRatio := computeComponents(g)
	coreness, maxCore := computeCoreness(g)

	coreCount := make(map[int]int, maxCore+1)
	coreMap := make(map[string]int, g.nodeCount())
	for i, c := range coreness {
		coreMap[g.nodes[i]] = c
		coreCount[c]++
	}

	return GraphStats{
		NodeCount:      g.nodeCount(),
		EdgeCount:      g.edgeCount(),
		ComponentCount: compCount,
		GiantRatio:     giantRatio,
		MaxCore:        maxCore,
		CoreCount:      coreCount,
		Coreness:       coreMap,
	}, nil
}

// computeComponents runs union-find over the graph and returns the number of
// connected components and the giant-component ratio (largest / total).
// Path-compression and union-by-rank keep the amortised cost near-linear.
func computeComponents(g *noteGraph) (count int, giantRatio float64) {
	n := g.nodeCount()
	parent := make([]int, n)
	rank := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x]) // path compression
		}
		return parent[x]
	}

	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra == rb {
			return
		}
		// Union by rank.
		switch {
		case rank[ra] < rank[rb]:
			parent[ra] = rb
		case rank[ra] > rank[rb]:
			parent[rb] = ra
		default:
			parent[rb] = ra
			rank[ra]++
		}
	}

	for i, nb := range g.adj {
		for _, j := range nb {
			union(i, j)
		}
	}

	// Count component sizes and locate the giant.
	compSize := make(map[int]int, n)
	for i := 0; i < n; i++ {
		compSize[find(i)]++
	}
	count = len(compSize)
	largest := 0
	for _, sz := range compSize {
		if sz > largest {
			largest = sz
		}
	}
	giantRatio = float64(largest) / float64(n)
	return count, giantRatio
}

// computeCoreness computes the k-core coreness for every node using the
// Batagelj-Zaversnik O(V+E) bucket-sort peel algorithm:
//
//  1. Initialise each node's effective degree from the adjacency list.
//  2. Arrange nodes in non-decreasing degree order via bucket sort.
//  3. Process in order: for node u with effective degree d, record
//     coreness[u] = d; then for each neighbour v still in the graph whose
//     effective degree exceeds d, decrement v's degree and move it one bucket
//     earlier.
//
// Returns the per-node coreness slice (indexed by nodeIdx) and the maximum
// coreness across all nodes.
func computeCoreness(g *noteGraph) (coreness []int, maxCore int) {
	n := g.nodeCount()
	coreness = make([]int, n)
	deg := make([]int, n)
	maxDeg := 0
	for i := range deg {
		deg[i] = g.degree(i)
		if deg[i] > maxDeg {
			maxDeg = deg[i]
		}
	}

	// Bucket sort: bin[d] holds the nodes with current effective degree d.
	bin := make([][]int, maxDeg+1)
	for i := range bin {
		bin[i] = []int{}
	}
	for i, d := range deg {
		bin[d] = append(bin[d], i)
	}

	// Build the flat order array and the per-node position map from the buckets.
	order := make([]int, n)
	pos := make([]int, n)
	idx := 0
	for d := 0; d <= maxDeg; d++ {
		for _, v := range bin[d] {
			order[idx] = v
			pos[v] = idx
			idx++
		}
	}

	// binStart[d] is the first index in order that belongs to bucket d.
	binStart := make([]int, maxDeg+2)
	binStart[0] = 0
	for d := 1; d <= maxDeg+1; d++ {
		binStart[d] = binStart[d-1] + len(bin[d-1])
	}

	removed := make([]bool, n)
	for i := 0; i < n; i++ {
		u := order[i]
		coreness[u] = deg[u]
		if coreness[u] > maxCore {
			maxCore = coreness[u]
		}
		removed[u] = true

		for _, v := range g.adj[u] {
			if removed[v] {
				continue
			}
			dv := deg[v]
			if dv > coreness[u] {
				// Move v to the start of its current bucket, then advance the
				// bucket start pointer (equivalent to moving v one bucket lower).
				pv := pos[v]
				bs := binStart[dv]
				w := order[bs]
				if w != v {
					order[pv], order[bs] = order[bs], order[pv]
					pos[v], pos[w] = bs, pv
				}
				binStart[dv]++
				deg[v]--
			}
		}
	}
	return coreness, maxCore
}

// componentMembers runs union-find and returns a slice of components, each
// component being a sorted slice of note paths. The result is sorted by
// component size ascending, then by first member path, for deterministic output.
func componentMembers(g *noteGraph) [][]string {
	n := g.nodeCount()
	if n == 0 {
		return nil
	}
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	for i, nb := range g.adj {
		for _, j := range nb {
			ra, rb := find(i), find(j)
			if ra != rb {
				parent[ra] = rb
			}
		}
	}
	groups := make(map[int][]string)
	for i, p := range g.nodes {
		root := find(i)
		groups[root] = append(groups[root], p)
	}
	comps := make([][]string, 0, len(groups))
	for _, members := range groups {
		sort.Strings(members)
		comps = append(comps, members)
	}
	// Stable sort: by size ascending, then by first member for determinism.
	sort.Slice(comps, func(i, j int) bool {
		if len(comps[i]) != len(comps[j]) {
			return len(comps[i]) < len(comps[j])
		}
		return comps[i][0] < comps[j][0]
	})
	return comps
}

// underPrefix reports whether path is under any of the given folder prefixes.
// A prefix "2-Areas" matches "2-Areas/foo.md" and "2-Areas/sub/bar.md" but
// NOT "not-2-Areas/foo.md". A trailing slash on the prefix is stripped before
// comparison.
func underPrefix(path string, prefixes []string) bool {
	for _, pfx := range prefixes {
		pfx = strings.TrimSuffix(pfx, "/")
		if strings.HasPrefix(path, pfx+"/") || path == pfx {
			return true
		}
	}
	return false
}

// detectOrphansAndLeaves returns findings for degree-0 (orphan) and degree-1
// (leaf) notes that satisfy ALL of:
//   - the note is under a connective folder (GetConnectiveFolders), AND
//   - the note is NOT under an expected-orphan folder (GetExpectedOrphanFolders), AND
//   - the note's mtime is older than OrphanStaleDays.
func detectOrphansAndLeaves(cfg Config, db *sql.DB, g *noteGraph) ([]Finding, error) {
	staleDays := cfg.Health.GetOrphanStaleDays()
	cutoffMs := float64(timeNowForTest().AddDate(0, 0, -staleDays).UnixNano()) / 1e6

	connective := cfg.Health.GetConnectiveFolders()
	excluded := cfg.Health.GetExpectedOrphanFolders()

	// Fetch mtime for every note to check age.
	rows, err := db.Query("SELECT path, mtime FROM notes ORDER BY path")
	if err != nil {
		return nil, fmt.Errorf("orphan detector: %w", err)
	}
	defer rows.Close()

	var findings []Finding
	for rows.Next() {
		var path string
		var mtime float64
		if err := rows.Scan(&path, &mtime); err != nil {
			return nil, fmt.Errorf("orphan detector: scan: %w", err)
		}

		// Never flag notes under expected-orphan folders.
		if underPrefix(path, excluded) {
			continue
		}
		// Only flag notes under connective folders.
		if !underPrefix(path, connective) {
			continue
		}
		// Only flag notes older than the stale threshold.
		if mtime >= cutoffMs {
			continue
		}

		idx, ok := g.nodeIdx[path]
		if !ok {
			continue
		}
		d := g.degree(idx)
		ageDays := msToAgeDays(mtime)

		switch d {
		case 0:
			findings = append(findings, Finding{
				Type:     "orphan",
				Path:     path,
				Detail:   fmt.Sprintf("degree 0 (no resolved links), age %d days", ageDays),
				Severity: "warn",
			})
		case 1:
			findings = append(findings, Finding{
				Type:     "leaf",
				Path:     path,
				Detail:   fmt.Sprintf("degree 1 (single resolved link), age %d days", ageDays),
				Severity: "warn",
			})
		}
	}
	return findings, rows.Err()
}

// detectIslands returns a finding for each small connected component that is a
// genuine isolated cluster. A component qualifies as an island finding only
// when ALL of the following hold:
//
//  1. size >= 2: single-note components (degree-0 orphans) are already reported
//     by detectOrphansAndLeaves; do not double-report them here.
//  2. size <= IslandMaxSize: the component is genuinely small.
//  3. NOT the largest component: any component whose size equals the maximum
//     component size in the graph is treated as the "mainland" and excluded.
//     This prevents flagging the hub-and-spoke giant component as an island on
//     small vaults where the giant happens to be <= IslandMaxSize.
//  4. Not all members are under an archive folder (GetArchiveFolders). Islands
//     in Journal or Notes are still reported; only archive folders suppress them.
func detectIslands(cfg Config, g *noteGraph) []Finding {
	maxSize := cfg.Health.GetIslandMaxSize()
	excluded := cfg.Health.GetArchiveFolders()

	comps := componentMembers(g)

	// Find the maximum component size so we can exclude the mainland.
	maxCompSize := 0
	for _, members := range comps {
		if len(members) > maxCompSize {
			maxCompSize = len(members)
		}
	}

	var findings []Finding
	for _, members := range comps {
		sz := len(members)

		// Condition 1: single-note components are orphans, not islands.
		if sz < 2 {
			continue
		}
		// Condition 2: only small components qualify.
		if sz > maxSize {
			continue
		}
		// Condition 3: exclude the mainland (every component tied for largest).
		if sz == maxCompSize {
			continue
		}
		// Condition 4: skip if every member is under an archive folder.
		// Note: this uses GetArchiveFolders (default ["4-Archives"]), NOT
		// GetExpectedOrphanFolders. Journal and Notes are exempt from orphan/leaf
		// flagging but their small islands are still reported.
		allArchived := true
		for _, p := range members {
			if !underPrefix(p, excluded) {
				allArchived = false
				break
			}
		}
		if allArchived {
			continue
		}
		findings = append(findings, Finding{
			Type:     "island",
			Path:     members[0],
			Detail:   fmt.Sprintf("isolated cluster of %d: %s", sz, strings.Join(members, ", ")),
			Severity: "warn",
		})
	}
	return findings
}

// msToAgeDays converts a float64 millisecond-since-epoch timestamp (as stored
// in the notes table) to whole days elapsed relative to the injected test clock.
// Mirrors the mtime conversion used in detectPARADrift.
func msToAgeDays(ms float64) int {
	return int(timeNowForTest().Sub(msToTime(ms)).Hours() / 24)
}

// msToTime converts a float64 millisecond-since-epoch value to a time.Time
// (UTC). The integer cast matches the inverse written in the indexer:
// float64(info.ModTime().UnixNano()) / 1e6 stored as milliseconds.
func msToTime(ms float64) time.Time {
	return time.Unix(0, int64(ms)*int64(time.Millisecond/time.Nanosecond)).UTC()
}
