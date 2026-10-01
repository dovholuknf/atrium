- **A card's replies page backwards.** `GET /v1/tasks/{id}/replies` takes `n` up to 50 and `before=<at>`, which
  answers the replies and prompts strictly older, oldest first. `more` says whether anything older is left, on the
  first page too, and `next_before` is the `before` to send for the next page: both lists are complete down to it, so
  a client never works out a cursor and nothing is lost or repeated. A deep page reads back from the `before` position
  in 2MB steps and stops at 16MB, then says `more`. A bad `before` answers 400. For /m's "load older". Room side,
  needs a room deploy. (with @ui)
