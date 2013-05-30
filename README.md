index-html
==========

A simple Go HTTP server to replace lighttpd's default file browser and downloader interface with additional features.

Features
---

 * Adds custom sort ability via two methods
   * Create a dummy file in the directory named `.index-sort` containing a single line with the value `**sort-method**`
   * Supply `?sort=**sort-method**` query-string parameter in request (overrides dummy file)
   * Folders are always sorted to display before files
   * Available sorting methods:
     * `name-asc`  sorts by file name in ascending order (default)
     * `name-desc` sorts by file name in descending order
     * `date-asc`  sorts by last modified time in ascending order
     * `date-desc` sorts by last modified time in descending order
 * 302 redirect support for relative symlinks
   * Requests for symlinks will 302 redirect to the target file (or folder) if that target is
     found within the filesystem root jail.

Arguments
---

  `./index-html <listen socket type> <listen address> <web root> <accel redirect> <filesystem root>`

Starts a Go HTTP server listening on a socket of type `<listen socket type` at `<listen address>` expecting
HTTP requests for paths starting with `<web root>`, serving requests for directory listings and/or file
downloads for filesystem objects found under `<filesystem root>`. `<accel redirect>` is used to provide the
`X-Accel-Redirect` header with the root path for nginx to pick up on.

chroot is not used to provide the filesystem root jail due to cross-platform compatibility concerns.

Upstart
---

An upstart script `index-html.conf` is included to daemonize `index-html` and redirect the stderr log
to a system log file.
