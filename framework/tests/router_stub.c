/* Test-only native definitions for router.src's `extern "env"` DOM imports.
 *
 * router.src is half a wasm binding: dom_html and nav_url are imports the browser
 * host supplies. Natively they resolve to nothing, and `export func nav` is ALWAYS
 * instantiated, so it drags them into the link whether a test calls them or not —
 * which made the module impossible to build under `machin test` at all.
 *
 * These no-ops let the pure routing logic (route / path_index / link /
 * current_route) be tested natively. They deliberately record nothing: asserting
 * on DOM side effects would be testing this stub, not router.src.
 */
void dom_html(const char *id, const char *html) { (void)id; (void)html; }
void nav_url(const char *path) { (void)path; }

/* An extern block must declare at least one fn, and a block's cflags are only
 * emitted when the block is used — so the suite calls this once to pull the stub
 * into the build. */
void router_stub_link(void) {}
