The paste spinner now stops when the room says the paste is in the pty, not on a guess from output. Every paste frame
carries an id and the room answers `in-done` with it. A socket that closes also ends it.
The box goes up before the frame is measured or sent, and the frame leaves after the box has painted.
An older room that never answers keeps the old guess, per socket, with the 20 second cap.
