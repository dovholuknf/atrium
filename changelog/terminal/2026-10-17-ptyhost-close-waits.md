The pty host's Close now waits for the goroutines it started, a pty's drain and await and a client's reader and
writer, so nothing logs after Close has returned. A package test run used to die on "Log in goroutine after test
has completed" when a runner's exit was logged after its test ended. ptyhost-close-waits.
