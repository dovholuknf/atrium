The pasteBig board unit reads the paste box's hold from the DOM, through a MutationObserver, rather than from
animation frames, so a renderer that stops painting under CI load no longer reads as a box that went down. Its
failure now carries the frame count, the longest frame gap and every flip of the box. paste-big-hold.
