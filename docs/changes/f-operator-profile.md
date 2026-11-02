## Test plan

## @LETTER@. The operator profile

### @LETTER@1. Default and write

1. GET `/_hub/profile` on a fresh hub and see the default author.
2. POST a profile from the hub's machine and GET it again.
3. Land from the hub and see the commit carry the profile's email, and a global git email that differs refused.
