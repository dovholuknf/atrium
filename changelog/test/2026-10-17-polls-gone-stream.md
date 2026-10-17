- `pollsGone` waits for the page's hub event stream to be open before it writes the `rooms` event to every open
  stream: on a loaded runner the page had not opened it yet and the event went nowhere. polls-gone-stream.
