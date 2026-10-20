Tests that push into a bare repository no longer race the auto maintenance receive-pack starts after the push. The shared test git config turns it off (chunked-post-maintenance-lock).
