## Test plan

## @LETTER@. The typing model cannot wedge shut

### @LETTER@1. A paste with its end marker lost

1. Open the typing readout on a Claude card.
2. In a terminal attached to it, send `ESC [ 200 ~`, some text, and no end marker.
3. Wait 3 seconds and press Enter, or press control-c right away.

**Expected:** the readout shows `in_paste` while the paste is open, and the line empties after the Enter or control-c.

### @LETTER@2. A submit empties a stuck model

1. With the model holding text it should not, send a message to the card from another session.

**Expected:** once the prompt hook fires, the readout shows an empty line. A draft typed after the submit stays.
