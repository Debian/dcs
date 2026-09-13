// © 2014 Michael Stapelberg
// vim:ts=4:sw=4:et
// Opens a WebSocket connection to Debian Code Search to send and receive
// search results almost instantaneously.

// NB: All of these constants needs to match those in cmd/dcs-web/querymanager.go
var packagesPerPage = 5;
var resultsPerPackage = 2;

var animationFallback;
var searchterm;
var ppHideAfterAnimation = false;

// fatal (bool): Whether all ongoing operations should be cancelled.
//
// permanent (bool): Whether this message will be displayed permanently (e.g.
// “search results incomplete” vs. “trying to reconnect in 3s…”)
//
// unique_id (string): If non-null, only one message of this type will be
// displayed. Can be used to display only one notification about incomplete
// search results, regardless of how many backends the server returns as
// unhealthy.
//
// message (string): The human-readable error message.
function error(fatal, permanent, unique_id, message) {
    if (unique_id !== null && $('#errors div[data-uniqueid=' + unique_id + ']').size() > 0) {
        return;
    }
    if (fatal) {
        progress(100, false, 'Error: ' + message);
        return;
    }

    var div = $('<div class="alert alert-' + (permanent ? 'danger' : 'warning') + '" role="alert"></div>');
    if (unique_id !== null) {
        div.attr('data-uniqueid', unique_id);
    }
    div.text(message);
    $('#errors').append(div);
    return div;
}

// Setting percentage to 0 means initializing the progressbar. To display some
// sort of progress to the user, we’ll set it to 10%, so any actual progress
// that is communicated from the server will need to be ≥ 10%.
//
// Setting temporary to true will reset the text to the last non-temporary text
// upon completion (which is a call with percentage == 100).
function progress(percentage, temporary, text) {
    var progressbar = document.getElementById('progressbar');
    var bar = progressbar.querySelector('.progress-bar');
    var span = progressbar.querySelector('span');
    if (percentage == 0) {
        span.textContent = text;
        bar.style.width = '10%';
        bar.classList.add('progress-active');
        progressbar.style.display = ''; // show
    } else {
        if (text !== null) {
            span.textContent = text;
            if (!temporary) {
                progressbar.dataset.oldText = text;
            }
        }
        bar.style.width = percentage + '%';
        if (percentage == 100) {
            bar.classList.remove('progress-active');
            if (temporary) {
                span.textContent = progressbar.dataset.oldText;
            }
        }
    }
}

function sendQuery(term, literal) {
    document.getElementById('normalresults').style.display = ''; // show
    document.getElementById('progressbar').style.display = ''; // show
    document.getElementById('options').style.display = 'none'; // hide
    document.getElementById('packageshint').style.display = 'none'; // hide
    var query = term;
    // EventSource is supported by Chrome 9+ and Firefox 6+.
    var eventsrc = new EventSource("/events/?q=" + query + "&literal=" + (literal ? "1" : "0"));
    eventsrc.onmessage = onEvent;
    document.title = searchterm + ' · Debian Code Search';
    progress(0, false, 'Checking which files to grep…');
}

var queryid;
var resultpages;
var currentpage;
var currentpage_pkg;
var packages = [];

// Called when clicking a search result link.
function track(ev) {
    var dnt = navigator.doNotTrack == "yes" ||
              navigator.doNotTrack == "1" ||
              navigator.msDoNotTrack == "1" ||
              window.doNotTrack == "1";
    if (dnt) {
        // Respect the user’s choice and don’t track.
        return;
    }

    var link = $(ev.currentTarget);
    navigator.sendBeacon("/track", new Blob(
        [JSON.stringify({
            "searchterm": searchterm,
            "path": link.attr('data-path'),
            "line": link.attr('data-line'),
          })],
        {"type": "application/json; charset=UTF-8"}));
}

function addSearchResult(results, result) {
    var context = [];

    // NB: All of the following context lines are already HTML-escaped by the server.
    context.push(result.ctxp2);
    context.push(result.ctxp1);
    context.push('<strong>' + result.context + '</strong>');
    context.push(result.ctxn1);
    context.push(result.ctxn2);
    // Remove any empty context lines (e.g. when the match is close to the
    // beginning or end of the file).
    context = $.grep(context, function(elm, idx) { return $.trim(elm) != ""; });
    // TODO: only replace \t at the beginning of each context line
    context = context.join("<br>").replace("\t", "    ");

    // Split the path into source package (bold) and rest.
    var delimiter = result.path.indexOf("_");
    var sourcePackage = result.path.substring(0, delimiter);
    var rest = result.path.substring(delimiter);

    // Append the new search result, then sort the results.
    var el = $('<li data-ranking="' + result.ranking + '"><a onclick="track(event);" href="/show?file=' + encodeURIComponent(result.path) + '&line=' + result.line + '"><code><strong>' + sourcePackage + '</strong>' + escapeForHTML(rest) + '</code></a><br><pre>' + context + '</pre><small>PathRank: ' + result.pathrank + ', Final: ' + result.ranking + '</small></li>');
    $(el).children('a').attr('data-path', result.path).attr('data-line', result.line);
    results.append(el);
    $('ul#results').append($('ul#results>li').detach().sort(function(a, b) {
        return b.getAttribute('data-ranking') - a.getAttribute('data-ranking');
    }));

    // For performance reasons, we always keep the amount of displayed
    // results at 10. With (typically rather generic) queries where the top
    // results are changed very often, the page would get really slow
    // otherwise.
    var items = $('ul#results>li');
    if (items.size() > 10) {
        items.last().remove();
    }
}

function loadPage(nr) {
    // There’s pagination at the top and at the bottom of the page. In case the
    // user used the bottom one, it makes sense to scroll back to the top. In
    // case the user used the top one, the scrolling won’t be noticed.
    scrollTo(0, 0);

    // Start the progress bar after 200ms. If the page was in the cache, this
    // timer will be cancelled by the load callback below. If it wasn’t, 200ms
    // is short enough of a delay to not be noticed by the user.
    var progress_bar_start = setTimeout(function() {
        progress(0, true, 'Loading search result page ' + (nr+1) + '…');
    }, 200);

    var pathname = pageUrl(nr, false);
    if (location.toString() !== pathname) {
        history.pushState({ searchterm: searchterm, nr: nr, perpkg: false }, 'page ' + nr, pathname);
    }
    fetch('/results/' + queryid + '/page_' + nr + '.json')
        .then(function(resp) {
            if (!resp.ok) {
                throw new Error(resp.statusText);
            }
            return resp.json();
        })
        .then(function(data) {
            clearTimeout(progress_bar_start);
            // TODO: experiment and see whether animating the results works
            // well. Fade them in one after the other, see:
            // http://www.google.com/design/spec/animation/meaningful-transitions.html#meaningful-transitions-hierarchical-timing
            currentpage = nr;
            updatePagination(currentpage, resultpages, false);
            $('ul#results>li').remove();
            var ul = $('ul#results');
            $.each(data, function(idx, element) {
                addSearchResult(ul, element);
            });
            progress(100, true, null);
        })
        .catch(function(err) {
            error(true, true, null, 'Could not load search query results: ' + err.message);
        });
}

// If preload is true, the current URL will not be updated, as the data is
// preloaded and inserted into (hidden) DOM elements.
function loadPerPkgPage(nr, preload) {
    var progress_bar_start;
    if (!preload) {
        // There’s pagination at the top and at the bottom of the page. In case the
        // user used the bottom one, it makes sense to scroll back to the top. In
        // case the user used the top one, the scrolling won’t be noticed.
        scrollTo(0, 0);

        // Start the progress bar after 20ms. If the page was in the cache,
        // this timer will be cancelled by the load callback below. If it
        // wasn’t, 20ms is short enough of a delay to not be noticed by the
        // user.
        progress_bar_start = setTimeout(function() {
            progress(0, true, 'Loading per-package search result page ' + (nr+1) + '…');
        }, 20);
        var pathname = pageUrl(nr, true);
        if (location.toString() !== pathname) {
            history.pushState({ searchterm: searchterm, nr: nr, perpkg: true }, 'page ' + nr, pathname);
        }
    }
    fetch('/results/' + queryid + '/perpackage_2_page_' + nr + '.json')
        .then(function(resp) {
            if (!resp.ok) {
                throw new Error(resp.statusText);
            }
            return resp.json();
        })
        .then(function(data) {
            if (progress_bar_start !== undefined) {
                clearTimeout(progress_bar_start);
            }
            currentpage_pkg = nr;
            updatePagination(currentpage_pkg, Math.ceil(packages.length / packagesPerPage), true);
            var pp = $('#perpackage-results');
            pp.text('');
            $.each(data, function(idx, meta) {
                pp.append('<h2>' + meta.Package + '</h2>');
                var ul = $('<ul></ul>');
                pp.append(ul);
                $.each(meta.Results, function(idx, result) {
                    addSearchResult(ul, result);
                });
                var u = new URL(location);
                var sp = new URLSearchParams(u.search.slice(1));
                sp.set('q', searchterm + ' package:\\Q' + meta.Package + '\\E');
                sp["delete"]('page');
                sp["delete"]('perpkg');
                u.search = "?" + sp.toString();
                var allResultsURL = u.toString();
                ul.append('<li><a href="' + allResultsURL + '">show all results in package <span class="packagename">' + meta.Package + '</span></a></li>');
                if (!preload) {
                    progress(100, true, null);
                }
            });
        })
        .catch(function(err) {
            error(true, true, null, 'Could not load search query results ("' + err.message + '").');
        });
}

function pageUrl(page, perpackage) {
    var u = new URL(location);
    var sp = new URLSearchParams(u.search.slice(1));
    if (page === 0) {
        sp["delete"]('page');
    } else {
        sp.set('page', page);
    }
    if (perpackage) {
        sp.set('perpkg', 1);
    } else {
        sp["delete"]('perpkg');
    }
    u.search = "?" + sp.toString();
    return u.toString();
}

function updatePagination(currentpage, resultpages, perpackage) {
    var clickFunc = (perpackage ? 'loadPerPkgPage' : 'loadPage');
    var html = '<strong>Pages:</strong> ';
    var start = Math.max(currentpage - 5, (currentpage > 0 ? 1 : 0));
    var end = Math.min((currentpage >= 5 ? currentpage + 5 : 10), resultpages);

    if (currentpage > 0) {
        html += '<a href="' + pageUrl(currentpage-1, perpackage) + '" onclick="' + clickFunc + '(' + (currentpage-1) + ');return false;" rel="prev">&lt;</a> ';
        html += '<a href="' + pageUrl(0, perpackage) + '" onclick="' + clickFunc + '(0);return false;">1</a> ';
    }

    if (start > 1) {
        html += '… ';
    }

    for (var i = start; i < end; i++) {
        html += '<a style="' + (i == currentpage ? "font-weight: bold" : "") + '" ' +
                'href="' + pageUrl(i, perpackage) + '" ' +
                'onclick="' + clickFunc + '(' + i + ');return false;">' + (i + 1) + '</a> ';
    }

    if (end < (resultpages-1)) {
        html += '… ';
    }

    if (end < resultpages) {
        html += '<a href="' + pageUrl(resultpages-1, perpackage) + '" onclick="' + clickFunc + '(' + (resultpages - 1) + ');return false;">' + resultpages + '</a>';
    }

    if (currentpage < (resultpages-1)) {
        html += '<link rel="prerender" href="' + pageUrl(currentpage+1, perpackage) + '">';
        html += '<a href="' + pageUrl(currentpage+1, perpackage) + '" onclick="' + clickFunc + '(' + (currentpage+1) + ');return false;" rel="next">&gt;</a> ';
    }

    $((perpackage ? '.perpackage-pagination' : '.pagination')).html(html);
}

function escapeForHTML(input) {
    return $('<div/>').text(input).html();
}

function getDefault(searchparams, name, def) {
    var val = searchparams.get(name);
    return (val === null ? def : val);
}

function onQueryDone(msg) {
    if (msg.Results === 0) {
        progress(100, false, msg.FilesTotal + ' files grepped (' + msg.Results + ' results)');
        var sp = new URLSearchParams(location.search.slice(1));
	var literal = sp.get('literal') === '1';
	if (literal) {
            error(false, true, 'noresults', 'Your query “' + searchterm + '” had no results. Did you mean to search in regex mode instead?');
	} else {
            error(false, true, 'noresults', 'Your query “' + searchterm + '” had no results. Please read the FAQ to make sure your syntax is correct.');
	}
        return;
    }

    document.getElementById('options').style.display = '';

    progress(100, false, msg.FilesTotal + ' files grepped (' + msg.Results + ' results)');

    // Request the results, but grouped by Debian source package.
    // Having these available means we can directly show them when the
    // user decides to switch to perpackage mode.
    loadPerPkgPage(0, true);

    fetch('/results/' + queryid + '/packages.json')
        .then(function(resp) {
            if (!resp.ok) {
                throw new Error(resp.statusText);
            }
            return resp.json();
        })
        .then(function(data) {
            var p = document.getElementById('packages');
            p.innerText = '';
            packages = data.Packages;
            updatePagination(currentpage_pkg, Math.ceil(packages.length / packagesPerPage), true);
            if (data.Packages.length === 1) {
                p.insertAdjacentHTML('beforeend', 'All results from Debian source package <strong>' + data.Packages[0] + '</strong>');
                document.getElementById('enable-perpackage').setAttribute('disabled', 'disabled');
                document.querySelector('label[for=enable-perpackage]').style.opacity = '0.5';
            } else if (data.Packages.length > 1) {
                // We are limiting the amount of packages because
                // some browsers (e.g. Chrome 40) will stop
                // displaying text with “white-space: nowrap” once
                // it becomes too long.
                var u = new URL(location);
                var sp = new URLSearchParams(u.search.slice(1));
                sp["delete"]('page');
                sp["delete"]('perpkg');
                var pkgLink = function(packageName) {
                    sp.set('q', searchterm + ' package:\\Q' + packageName + '\\E');
                    u.search = "?" + sp.toString();
                    return '<a href="' + u.toString() + '">' + packageName + '</a>';
                };
                var packagesList = data.Packages.slice(0, 1000).map(pkgLink).join(', ');
                p.insertAdjacentHTML('beforeend', '<span><strong>Filter by package</strong>: ' + packagesList + '</span>');
                if (p.querySelector('span:first-child').scrollWidth > p.clientWidth) {
                    p.insertAdjacentHTML('beforeend', '<span class="showhint"><a href="#" onclick="document.getElementById(\'packageshint\').style.display = \'\'; return false;">▾</a></span>');
                    var hint = document.getElementById('packageshint');
                    hint.textContent = '';
                    hint.insertAdjacentHTML('beforeend', 'To see all packages which contain results: <pre>curl -s ' + location.protocol + '//' + location.host + '/results/' + queryid + '/packages.txt</pre>');
                }

                document.getElementById('enable-perpackage').removeAttribute('disabled');
                document.querySelector('label[for=enable-perpackage]').style.opacity = '1.0';

                if (location.pathname.lastIndexOf('/perpackage-results/', 0) === 0) {
                    var parts = new RegExp("/perpackage-results/([^/]+)/2/page_([0-9]+)").exec(location.pathname);
                    document.getElementById('enable-perpackage').checked = true;
                    changeGrouping();
                    loadPerPkgPage(parseInt(parts[2]), false);
                }

                if (location.pathname === '/search') {
                    var u = new URL(location);
                    var sp = new URLSearchParams(u.search.slice(1));
                    if (sp.get('perpkg') !== '1') {
                        return;
                    }
                    document.getElementById('enable-perpackage').checked = true;
                    changeGrouping();
                    loadPerPkgPage(parseInt(getDefault(sp, 'page', 0)), false);
                }
            }
        })
        .catch(function(err) {
            error(true, true, null, 'Loading search result package list failed: ' + err.message);
        });
}

function onEvent(e) {
    var msg = JSON.parse(e.data);
    switch (msg.Type) {
        case "progress":
        queryid = msg.QueryId;

        progress(((msg.FilesProcessed / msg.FilesTotal) * 90) + 10,
                 false,
                 msg.FilesProcessed + ' / ' + msg.FilesTotal + ' files grepped (' + msg.Results + ' results)');
        if (msg.FilesProcessed == msg.FilesTotal) {
            this.close();
            onQueryDone(msg);
        }
        break;

        case "pagination":
        // Store the values in global variables for constructing URLs when the
        // user requests a different page.
        resultpages = msg.ResultPages;
        queryid = msg.QueryId;
        currentpage = 0;
        currentpage_pkg = 0;
        updatePagination(currentpage, resultpages, false);

        if (location.pathname.lastIndexOf('/results/', 0) === 0) {
            var parts = new RegExp("/results/([^/]+)/page_([0-9]+)").exec(location.pathname);
            loadPage(parseInt(parts[2]));
        }
        if (location.pathname === '/search') {
            var u = new URL(location);
            var sp = new URLSearchParams(u.search.slice(1));
            if (sp.get('perpkg') !== null) {
                break;
            }
            loadPage(parseInt(getDefault(sp, 'page', 0)));
        }
        break;

        case "error":
        if (msg.ErrorType == "backendunavailable") {
            error(false, true, msg.ErrorType, "The results may be incomplete, not all Debian Code Search servers are okay right now.");
        } else if (msg.ErrorType == "cancelled") {
            error(false, true, msg.ErrorType, "This query has been cancelled by the server administrator (to preserve overall service health).");
        } else if (msg.ErrorType == "failed") {
            error(false, true, msg.ErrorType, "This query failed due to an unexpected internal server error.");
        } else if (msg.ErrorType == "invalidquery") {
            error(false, true, msg.ErrorType, "This query was refused by the server: " + msg.ErrorMessage);
        } else {
            error(false, true, msg.ErrorType, msg.ErrorType);
        }
        this.close();
        onQueryDone(msg);
        break;

        default:
        addSearchResult($('ul#results'), msg);
        break;
    }
}

function setPositionAbsolute(selector) {
    var element = $(selector);
    var pos = element.position();
    pos.width = element.width();
    pos.height = element.height();
    pos.position = 'absolute';
    element.css(pos);
}

function setPositionStatic(selector) {
    $(selector).css({
        'position': 'static',
        'left': '',
        'top': '',
        'width': '',
        'height': ''});
}

function animationSupported() {
    var elm = $('#perpackage')[0];
    var prefixes = ["webkit", "MS", "moz", "o", ""];
    for (var i = 0; i < prefixes.length; i++) {
        if (elm.style[prefixes[i] + 'AnimationName'] !== undefined) {
            return true;
        }
    }
    return false;
}

// Switch between displaying all results and grouping search results by Debian
// source package.
function changeGrouping() {
    var perpackage = document.getElementById('perpackage');

    var currentPerPkg = perpackage.style.display !== 'none';
    var shouldPerPkg = document.getElementById('enable-perpackage').checked;
    if (currentPerPkg === shouldPerPkg) {
        return;
    }

    ppHideAfterAnimation = !shouldPerPkg;

    if (currentPerPkg) {
        perpackage.classList.add('animation-reverse');
    } else {
        perpackage.classList.remove('animation-reverse');
        perpackage.style.display = ''; // show
    }

    var u = new URL(location);
    var sp = new URLSearchParams(u.search.slice(1));
    if (shouldPerPkg) {
        perpackage.classList.remove('animation-reverse');
        sp.set('perpkg', 1);
        u.search = "?" + sp.toString();
        var pathname = u.toString();
        if (location.toString() != pathname) {
            history.pushState(
                { searchterm: searchterm, nr: currentpage_pkg, perpkg: true },
                'page ' + currentpage_pkg,
                pathname);
        }

        setPositionAbsolute('#footer');
        setPositionAbsolute('#normalresults');
        perpackage.style.display = ''; // show
    } else {
        perpackage.classList.add('animation-reverse');
        sp["delete"]('perpkg');
        u.search = "?" + sp.toString();
        var pathname = u.toString();
        if (location.toString() != pathname) {
            history.pushState(
                { searchterm: searchterm, nr: currentpage, perpkg: false },
                'page ' + currentpage,
                pathname);
        }
        document.getElementById('normalresults').style.display = ''; // show
        // For browsers that don’t support animations, we need to have a fallback.
        // The timer will be cancelled in the animationstart event handler.
        if (!animationSupported()) {
            animationFallback = setTimeout(function() {
                perpackage.style.display = 'none'; // hide
                setPositionStatic('#footer, #normalresults');
            }, 100);
        }
    }

    perpackage.classList.remove('ppanimation');
    // Trigger a reflow, otherwise removing/adding the animation class does not
    // lead to restarting the animation.
    perpackage.offsetWidth = perpackage.offsetWidth;
    perpackage.classList.add('ppanimation');
}

window.addEventListener('load', function() {
    if ('serviceWorker' in navigator) {
        navigator.serviceWorker.register('/service-worker.min.js?12');
    }

    // Pressing “/” anywhere on the page focuses the search field.
    document.addEventListener('keydown', function(e) {
        if (e.key == '/') {
            var q = $('#searchbox input[name=q], #searchform input[name=q]');
            if (q.is(':focus')) {
                return;
            }
            q.focus();
            e.preventDefault();
        }
    });

    var ppresults = document.getElementById('perpackage');
    ppresults.addEventListener('animationstart', function(e) {
        clearTimeout(animationFallback);
    });
    ppresults.addEventListener('animationend', function(e) {
        if (ppHideAfterAnimation) {
            ppresults.style.display = 'none';
            setPositionStatic('#footer, #normalresults');
        } else {
            document.getElementById('normalresults').style.display = 'none';
        }
    });

    // Recognize old URL patterns for backwards compatibility:
    if (location.pathname.lastIndexOf('/results/', 0) === 0 ||
        location.pathname.lastIndexOf('/perpackage-results/', 0) === 0) {
        var parts = new RegExp("results/([^/]+)").exec(location.pathname);
        searchterm = decodeURIComponent(parts[1]);
        sendQuery(parts[1], false);
    }

    if (location.pathname === '/search') {
        var sp = new URLSearchParams(location.search.slice(1));
        searchterm = sp.get('q');
	var literal = sp.get('literal') === '1';
	$('#searchbox option[value=' + (literal ? '1' : '0') + ']').prop('selected', true);
        sendQuery(encodeURIComponent(searchterm), literal);
    }

    // This is triggered when the user navigates (e.g. via back button) between
    // pages that were created using history.pushState().
    window.addEventListener('popstate', function(ev) {
        var sp = new URLSearchParams(location.search.slice(1));
        var perpkg = (sp.get('perpkg') === '1');
        var nr = getDefault(sp, 'page', 0);
        $('#enable-perpackage').prop('checked', perpkg);
        changeGrouping();
        if (perpkg) {
            loadPerPkgPage(nr);
        } else {
            loadPage(nr);
        }
    });
});
