# internal/base/safepath

One question: is this path inside that directory. One function, three rules, and it exists because nothing else
in atrium answered it.

## The three rules, and why each is there

1. **Symlinks are resolved on BOTH sides.** Resolving only the candidate lets a symlinked root compare false;
   resolving only the root lets a symlink out of the tree compare true.
2. **The comparison is on a separator boundary.** `/home/work` must not contain `/home/workspace`, which a
   prefix match says it does.
3. **Case folds on Windows only.** Folding everywhere makes two genuinely different paths on Linux look like
   one; folding nowhere makes `C:\Work` and `c:\work` look different on the platform where they are not.

## Why it is a package

`docs/runtime/file-transfer-design.md` records the mistake: `browse.go` looked like the containment answer and was
not one. Every caller that assumed it was had to be found and changed. Containment is now one function that says so
in its name, rather than a property somebody believed another function had.

A path that fails these rules answers 403 whether or not it exists, so an endpoint using this is not an oracle
for what is on the machine.
