`atrium run --transport zrok` starts when the zrok controller cannot be asked. It keeps the saved share, retries on a
backoff with one log line per attempt, and shows zrok down with why on the board. bug-zrok-hub-exits-when-controller-down.
