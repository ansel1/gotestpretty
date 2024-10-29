# gotestpretty

A command line tool for summarizing the results of go test, in real time.

## Installation

    go install github.com/ansel1/gotestpretty

## Usage

Pipe `go test -json` into `gotestpretty`:

    go test -json ./... | gotestpretty

...or, capture the output `go test -json` to a file, then summarize it:

    go test -json ./... > test.out
    gotestpretty -f test.out

Advanced usage, good for CI, handles some edge cases:

    set -euo pipefail
    go test -json ./... 2>&1 | gotestpretty

To see help and available options, like highlighting slow tests:

    gotestpretty -h

Anything piped to `gotestpretty` which doesn't appear to be `go test -json` output is just
passed directly to output, so you can pipe any output which has test output embedded in it:

    make all | gotestpretty

## Why?

Other tools exist that do similar stuff, but most don't give real time feedback while the tests are running.  Or are
hard to use when the commands are embedded in build scripts or makefiles.  Or I just preferred a different style of formatting.  `gotestpretty`'s formatting is inspired by JetBrains Goland's test runner UI.

Some other tools you can try:

- [tparse](https://github.com/mfridman/tparse)
- [gotestsum](https://github.com/gotestyourself/gotestsum)
- [gotestfmt](https://github.com/GoTestTools/gotestfmt?tab=readme-ov-file)

## TODO

- [x] Exit code if tests fail
- [x] Keep printing output after tests complete until pipe is empty
- [x] print out the test output at end of run
- [ ] detect and report panics
- [x] don't print anything until the first test starts
- [x] bottom align the output maybe?
- [x] don't skip long tests
- [x] what if finished packages sorted to the top, and the summary line was pinned to the bottom?
- [x] make the output more closely match non-verbose output
- [x] -help
- [x] finalize the name
- [x] stable sort of the finished packages (when packages finish, they currently jump from the bottom to whenever they started.  Would be less jumpy if finished packages were sorted in the order they finished.)
- [x] include coverage if present
- [x] skipped subtests are not printed because the parent test is not marked as skipped
- [x] print final summary even when ctrl-c
- [x] when a tty is allocated, and output from another process is piped to us, and that output includes stderr, formatting gets all messed
- [x] if test names have slashes in them, it creates phantom nodes
- [ ] if passed a file, and not replaying, maybe just skip the TUI completely and skip straight to the summary
- [ ] README
- [ ] license
- [ ] when dumping output of finished package, include a header showing which package it is (currently, tests are printed with no indication which package they are part of)