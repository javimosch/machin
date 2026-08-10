# regex bundle

The Windows target cannot use POSIX `<regex.h>`, so we bundle the single-header
Remimu engine by wareya: https://github.com/wareya/Remimu

Remimu is released under Creative Commons Zero (public domain).

The vendored `remimu.h` has one mechanical patch:
`#define IF_VERBOSE(X) { (void)0; }` replaces the verbose-debug macro so the
`printf` calls are removed at the preprocessor level, keeping generated-C builds
warning-free and avoiding empty-else syntax errors.
