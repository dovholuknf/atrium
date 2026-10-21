Fixed a held say that could wait seconds longer than it should after a new-context cycle: a say held while a retry was
mid-drain, or a retry that straddled the end of the cycle, widened the backoff to 5 seconds. Item q-says-queued-cycle.
