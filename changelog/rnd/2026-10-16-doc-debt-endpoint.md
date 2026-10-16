- The hub serves `GET /_hub/doc-debt`: how many open `docs` items count, how many pages they name, the age of the oldest,
  and whether the threshold is crossed. An item counts from its feature's `landed` step, or from filing when it has no
  work row. The thresholds are the hub settings `doc_debt.items` (8) and `doc_debt.days` (10). Nothing draws it yet.
  m-doc-debt-from-features.
