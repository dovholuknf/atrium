## Test plan

## @LETTER@. A fetch that keeps moving is not cut

### @LETTER@1. Many small chunks

1. Run `go test -tags "integration git" ./internal/gitsync -run TestAFetchThatKeepsMakingProgressIsNotCut -count=50`.

**Expected:** every run passes, on a loaded machine too, with the whole 600 byte body delivered.
