//go:build bindings

package main

// Wails generates its JavaScript bindings by compiling this program with the
// `bindings` tag and running it on the build machine. That run must not behave
// like the app.
//
// It did: it bound the event API port, so `wails build` failed outright
// whenever a pet was already running — which is precisely when somebody
// rebuilds. A fresh clone built fine and the same clone failed a minute later,
// for a reason the error message pinned on the wrong thing.
const generatingBindings = true
