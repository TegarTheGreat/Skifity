"""Resolve a container image to a tag that names a version.

A floating tag is not a version, so every template in the catalogue has to name
one. Upstreams disagree about how: some publish a major series (`1`, `5-alpine`)
that takes patches, some publish only exact releases. This asks the registry and
prefers, in order: an existing major or major.minor series tag, then the newest
exact semver release.

Run: python3 hack/resolve_tags.py < images.json > resolved.json
"""
import json, re, sys, urllib.request, urllib.error

TIMEOUT = 25
SKIP = re.compile(r'(nightly|beta|alpha|rc|dev|edge|canary|test|snapshot|unstable|insiders|preview)', re.I)
SEMVER = re.compile(r'^v?(\d+)\.(\d+)(?:\.(\d+))?(-[a-z0-9.]+)?$', re.I)


def get(url, headers=None):
    request = urllib.request.Request(url, headers=headers or {})
    with urllib.request.urlopen(request, timeout=TIMEOUT) as response:
        return json.load(response)


def hub_tags(repo):
    if "/" not in repo:
        repo = "library/" + repo
    out, url = [], f"https://hub.docker.com/v2/repositories/{repo}/tags/?page_size=100&ordering=last_updated"
    for _ in range(3):
        data = get(url)
        out += [t["name"] for t in data.get("results", [])]
        url = data.get("next")
        if not url:
            break
    return out


def registry_tags(host, repo):
    """GHCR, quay and lscr speak the v2 API; GHCR needs an anonymous token."""
    # lscr.io is a mirror of linuxserver's images; ask the registry that has them.
    if host == "lscr.io":
        host, repo = "ghcr.io", repo
    headers = {}
    if host == "ghcr.io":
        token = get(f"https://{host}/token?scope=repository:{repo}:pull&service={host}")["token"]
        headers["Authorization"] = "Bearer " + token
    return get(f"https://{host}/v2/{repo}/tags/list?n=500", headers).get("tags") or []


def tags_for(image):
    ref = image.split("@")[0]
    # docker.io is Docker Hub written out; its v2 API needs auth, the hub API does not.
    for prefix in ("index.docker.io/", "docker.io/", "registry-1.docker.io/"):
        if ref.startswith(prefix):
            ref = ref[len(prefix):]
    # lscr.io mirrors linuxserver's images, and the same images are on Docker
    # Hub, whose API returns them newest first. The v2 tags/list is unordered
    # and truncates, which picked a four-year-old release.
    if ref.startswith("lscr.io/linuxserver/"):
        ref = "linuxserver/" + ref.split("/")[-1]
    ref = ref.rsplit(":", 1)[0] if ":" in ref.rsplit("/", 1)[-1] else ref
    parts = ref.split("/")
    if "." in parts[0] or ":" in parts[0]:
        return registry_tags(parts[0], "/".join(parts[1:])), ref.replace("lscr.io/", "ghcr.io/")
    return hub_tags(ref), ref


def best(tags, variant):
    """Pick the tag that names a version, preferring a series over an exact one."""
    usable = [t for t in tags if not SKIP.search(t) and t not in ("latest", "main", "master", "stable")]
    if variant:
        same = [t for t in usable if t.endswith("-" + variant)]
        if same:
            usable = same

    releases = []
    for tag in usable:
        base = tag[: -len("-" + variant)] if variant and tag.endswith("-" + variant) else tag
        match = SEMVER.match(base)
        if match and not match.group(4):
            releases.append((tuple(int(g or 0) for g in match.groups()[:3]), tag, base))
    if not releases:
        return None
    releases.sort()
    version, newest, base = releases[-1]

    # A series tag that exists and tracks this release is better than pinning a
    # patch: it takes fixes without taking a new major.
    for series in (f"{version[0]}.{version[1]}", str(version[0])):
        for candidate in ({series, "v" + series} if base.startswith("v") else {series}):
            full = candidate + ("-" + variant if variant else "")
            if full in tags:
                return full
    return newest


def resolve(image):
    ref = image.split("@")[0]
    for prefix in ("index.docker.io/", "docker.io/", "registry-1.docker.io/"):
        if ref.startswith(prefix):
            ref = ref[len(prefix):]
            image = ref
    if ref.startswith("lscr.io/linuxserver/"):
        ref = image = "linuxserver/" + ref.split("/")[-1]
    name, _, tag = ref.rpartition(":")
    if "/" in tag:
        name, tag = ref, ""
    variant = ""
    for suffix in ("alpine", "slim", "apache", "fpm", "debian"):
        if tag.endswith(suffix) and tag != suffix:
            variant = suffix
    if tag and tag not in ("latest", "main", "master", "stable") and not SKIP.search(tag):
        base = tag[: -len("-" + variant)] if variant and tag.endswith("-" + variant) else tag
        if SEMVER.match(base) or re.match(r'^\d', base):
            return image, "already pinned"

    tags, repo = tags_for(image)
    picked = best(tags, variant or (tag if tag in ("alpine", "slim", "apache") else ""))
    if not picked:
        return None, f"no versioned tag among {len(tags)} tags"
    return f"{repo}:{picked}", "resolved"


def exists(image):
    """Ask the registry for the manifest. A tag this resolver invented, or one
    that was deleted since, must not reach the catalogue."""
    ref = image.split("@")[0]
    name, _, tag = ref.rpartition(":")
    parts = name.split("/")
    if "." in parts[0]:
        host, repo = parts[0], "/".join(parts[1:])
    else:
        host, repo = "registry-1.docker.io", (name if "/" in name else "library/" + name)

    service = {"registry-1.docker.io": "registry.docker.io"}.get(host, host)
    # A tag can contain a colon-free timestamp; rpartition already split on the
    # last colon, which is right, but an image with no tag at all leaves the
    # name in `tag`. Guard it rather than asking for a manifest called "".
    if not tag or "/" in tag:
        return False
    headers = {"Accept": "application/vnd.oci.image.index.v1+json,"
                         "application/vnd.docker.distribution.manifest.list.v2+json,"
                         "application/vnd.docker.distribution.manifest.v2+json"}
    try:
        auth = {"registry-1.docker.io": "https://auth.docker.io/token",
                "ghcr.io": "https://ghcr.io/token",
                "lscr.io": "https://ghcr.io/token",
                "quay.io": "https://quay.io/v2/auth"}.get(host)
        if auth:
            token = get(f"{auth}?scope=repository:{repo}:pull&service={service}").get("token")
            if token:
                headers["Authorization"] = "Bearer " + token
        request = urllib.request.Request(f"https://{host}/v2/{repo}/manifests/{tag}",
                                         headers=headers, method="HEAD")
        with urllib.request.urlopen(request, timeout=TIMEOUT) as response:
            return response.status == 200
    except urllib.error.HTTPError as err:
        if err.code != 401:
            return False
        # Some registries answer 401 to an anonymous token request and then
        # hand out a token from the header of the challenge. One retry with it.
        challenge = err.headers.get("WWW-Authenticate", "")
        realm = re.search(r'realm="([^"]+)"', challenge)
        service_hint = re.search(r'service="([^"]+)"', challenge)
        if not realm:
            return False
        try:
            url = f"{realm.group(1)}?scope=repository:{repo}:pull"
            if service_hint:
                url += "&service=" + service_hint.group(1)
            token = get(url).get("token") or get(url).get("access_token")
            headers["Authorization"] = "Bearer " + token
            request = urllib.request.Request(f"https://{host}/v2/{repo}/manifests/{tag}",
                                             headers=headers, method="HEAD")
            with urllib.request.urlopen(request, timeout=TIMEOUT) as response:
                return response.status == 200
        except Exception:
            return False
    except Exception:
        return False


if __name__ == "__main__":
    images = json.load(sys.stdin)
    out = {}
    for key, image in images.items():
        try:
            pinned, why = resolve(image)
        except Exception as exc:  # a registry that refuses is a template we drop
            pinned, why = None, f"{type(exc).__name__}: {exc}"
        if pinned and not exists(pinned):
            pinned, why = None, "the resolved tag does not exist"
        out[key] = {"from": image, "to": pinned, "why": why}
        print(f"{key:36} {image:55} -> {pinned or 'SKIP'} ({why})", file=sys.stderr)
    json.dump(out, sys.stdout, indent=1)
