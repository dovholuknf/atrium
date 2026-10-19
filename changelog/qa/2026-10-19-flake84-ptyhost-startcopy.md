TestStartRunsADetachedHostFromACopy no longer waits out a 3 s idle exit: the host is pinned by a hello while the test
looks at it and idles for 500 ms after. Test only, flake84.
