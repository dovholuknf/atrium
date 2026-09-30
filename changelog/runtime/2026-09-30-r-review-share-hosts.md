- **A listener that is not loopback answers only the names it is known by.** A share answers its own frontend name
  and loopback, a board bound to one address answers that address, a board bound to every interface answers this
  machine's addresses and host name, and `ATRIUM_HOSTS` adds names to all of them. DNS rebinding worked on those
  before, because a rebound page is same origin. A ziti service's name is its intercept address, which atrium cannot
  see, so it answers the names in `ATRIUM_HOSTS`, and with none it says once in the log that it answers any Host.
  Hub and room side. (r-new-review-5edc1821)
