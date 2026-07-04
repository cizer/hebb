---
title: Now
tags: [pin-board]
---

# Now

This is the front-and-centre pin board: a short list of things that matter
right now, not a durable index. Pins decay by design — each one carries a
`review-by` date, and a stale pin is a sign to refresh it or take it down, not
a badge of importance to defend.

**The contract:** each pin is an H2. The first line under the heading is
`review-by: YYYY-MM-DD`; free prose follows. `hebb audit` flags any pin whose
review-by date has passed as `stale_pin`. When that happens, either bump the
date (the pin still matters) or delete the section (it doesn't). Don't add a
pin without a review-by line, and don't let a flagged one linger unactioned.

## Example: renew the Q3 vendor contract (example — replace or remove)

review-by: 2099-12-31

This is a placeholder pin showing the shape, not a real item. Replace it with
your own front-and-centre pins, or delete it once you've added at least one
real one. Real pins get a real, near-term review-by date (a week or two out is
typical); the far-future date here only keeps this example from flagging as
stale before you've replaced it.
