// Package storage is a small, typed key/value persistence wrapper for a
// browser game client.
//
// A Backend is the raw byte store — the browser build persists to
// window.localStorage; every other build (including native tests) uses an
// in-memory Backend, so game logic that reads and writes through a Store can
// be unit-tested without a browser.
//
// Namespaced wraps a Backend with a fixed key prefix, so unrelated features
// (hub resume tokens, settings, save data) sharing one localStorage origin
// cannot collide. Every Backend method reports failure instead of panicking:
// localStorage can throw in private-browsing mode, when a quota is exceeded,
// or when it is simply unavailable, and a game should keep running with
// storage silently disabled rather than crash.
package storage
