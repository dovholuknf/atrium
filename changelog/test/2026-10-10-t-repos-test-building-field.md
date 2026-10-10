# The repos list test knows the snapshot object

`TestReposIsOpenLikeGrowlsAndExactlyShaped` expected a bare `{"repos":[]}` and failed since the repos page became fast.
The answer stays `{"building":...,"repos":[...]}`, which the board reads, and the test now checks the `repos` list and
that `building` is there. Item t-repos-test-building-field.
