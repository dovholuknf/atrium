- Tests have a bucket file, `scripts/test-buckets.json`: one bucket per board page and per API surface. The touched-file
  planner reads it first and names the bucket, and a lint fails when a new board, api or link file, or a headless unit,
  is in no bucket. No test moved. rd-test-layout, stage 0.
