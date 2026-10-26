## Test plan

## @LETTER@. Runner contract conformance kit

### @LETTER@1. Claude's hooks pass

1. Run `atrium adapter test --runner claude`.

**Expected:** four `pass` lines (ok, refuse, hang, garbage), `features: session yes, activity yes, permission yes,
stop yes, finish no`, and exit 0. The hang run takes about 12 seconds.

### @LETTER@2. A room says its versions

1. With a room running, `curl <agent address>/contract`.

**Expected:** `{"versions":[1]}`.
