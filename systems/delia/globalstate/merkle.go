package globalstate

import "delia/merkle"

// mhtGen is a package-level indirection to merkle.MHTGen. It exists only so
// tests can instrument call counts (ALG2-002: "exactly one MHTGen call per
// StateVerify call"); it does not add caching or change behavior. Tests that
// override it must restore merkle.MHTGen afterward.
var mhtGen = merkle.MHTGen
