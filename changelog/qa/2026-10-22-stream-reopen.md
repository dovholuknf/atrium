A board opening its event stream the instant the last one closed could end up streaming from no room: the closing run's cancel now happens under the lock the next client takes (stream-reopen).
