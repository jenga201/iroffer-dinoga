// Package web implements the built-in HTTP web interface: an HTML pack
// listing, case-insensitive search, a JSON listing (/list.json), per-pack MD5
// lookup (/md5/<name>), an SFV summary (/sfv), and optional file downloads
// (/dl/<name>), all guarded by optional IP allow/deny CIDR lists.
//
// Build a handler with New and mount it under an http.Server.
package web
