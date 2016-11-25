// vim:ts=4:sw=4:et
var version = 'v2/';

var assets = {
    '/non-critical.min.css': true,
    '/Pics/openlogo-50.svg': true,
    '/Pics/rackspace.svg': true,
    '/jquery.min.js': true,
    '/url-search-params.min.js': true,
    '/loadCSS.min.js': true,
    '/cssrelpreload.min.js': true,
    '/instant.min.js?9': true,
    // Only cache fonts in woff2 format, all browsers which support service
    // workers also support woff2.
    '/Inconsolata.woff2': true,
    '/Roboto-Regular.woff2': true,
    '/Roboto-Bold.woff2': true,
    '/placeholder.html?2': true
};

var entityMap = {
  "&": "&amp;",
  "<": "&lt;",
  ">": "&gt;",
  '"': '&quot;',
  "'": '&#39;',
  "/": '&#x2F;'
};

function escapeHtml(string) {
  return String(string).replace(/[&<>"'\/]/g, function (s) {
    return entityMap[s];
  });
}

self.addEventListener("install", function(event) {
    event.waitUntil(
        caches
            .open(version + 'assets')
            .then(function(cache) {
                return cache.addAll(Object.keys(assets));
            })
    );
});

self.addEventListener("activate", function(event) {
    // The new version of the service worker is activated, superseding any old
    // version. Go through the cache and delete all assets whose key doesn’t
    // start with the current version prefix.
    event.waitUntil(
        caches
            .keys()
            .then(function(keys) {
                return Promise.all(
                    keys
                        .filter(function(key) {
                            return !key.startsWith(version);
                        })
                        .map(function(key) {
                            return caches['delete'](key);
                        })
                );
            })
    );
});

self.addEventListener("fetch", function(event) {
    if (event.request.method !== 'GET') {
        return;
    }
    var u = new URL(event.request.url);
    if (assets[u.pathname + u.search] === true) {
        event.respondWith(caches.match(event.request).then(function(response) {
            // Defense in depth: in case the cache request fails, fall back to
            // fetching the request.
            return response || fetch(event.request);
        }));
        return;
    }
    if (u.pathname === '/search') {
        event.respondWith(caches.match('/placeholder.html?2').then(function(response) {
            if (!response) {
                return fetch(event.request);
            }
            // URLSearchParams is not available on all browsers yet.
            var searchParams = u.search.slice(1).split('&');
            var q;
            var qEscaped;
            for (var i = 0, len = searchParams.length; i < len; i++) {
                if (searchParams[i].indexOf('q=') === 0) {
                    qEscaped = searchParams[i].substr('q='.length);
                    q = decodeURIComponent(qEscaped.replace(/\+/g, ' '));
                    break;
                }
            }
            if (q === undefined) {
                return response;
            }

            // HTML escape q and qEscaped so that they are safe to
            // string-replace into the placeholder document.
            q = escapeHtml(q);
            qEscaped = escapeHtml(qEscaped);

            var init = {
                status: response.status,
                statusText: response.statusText,
                headers: {},
            };
            response.headers.forEach(function(v, k) {
                init.headers[k] = v;
            });
            return response.text().then(function(body) {
                var replaced = body.replace(/%q%/g, q);
                replaced = replaced.replace(/%q_escaped%/g, qEscaped);
                return new Response(replaced, init);
            });
        }));
        return;
    }
    return;
});
