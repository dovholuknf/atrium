The board no longer queues a second /v1/tasks read behind one still in flight, so task events on a slow answer stay within the once-per-5s throttle.
