The ptyhost ring-cuts test reads the runner's stream until its burst has landed before it resizes, instead of
polling the output offset past a number, so a loaded box that lands the burst in several reads no longer puts the
resize mid-burst and leaves the test asserting a cut the ring was right to drop. ring-cuts-burst-lands.
