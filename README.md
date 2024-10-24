## TODO

- [x] Exit code if tests fail
- [x] Keep printing output after tests complete until pipe is empty
- [x] print out the test output at end of run
- [ ] detect and report panics
- [x] don't print anything until the first test starts
- [x] bottom align the output maybe?
- [ ] don't skip long tests
- [x] what if finished packages sorted to the top, and the summary line was pinned to the bottom?
- [x] make the output more closely match non-verbose output
- [ ] -help
- [ ] finalize the name
- [ ] stable sort of the finished packages (when packages finish, they currently jump from the bottom to whenever they started.  Would be less jumpy if finished packages were sorted in the order they finished.)
- [x] include coverage if present