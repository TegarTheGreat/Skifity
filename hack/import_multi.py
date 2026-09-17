"""Convert Coolify's multi-service templates into Skifity catalogue files.

Skifity already installs several apps from one template — `installTemplate`
creates an app per service and links the databases afterwards — so this is a
converter problem rather than a product one. Apps in an environment share a
namespace and reach each other by name, which is what makes the references
between these services work here at all.

What it refuses matters more than what it converts:

  * A service needing the Docker socket cannot run. Containers here are
    unprivileged with every capability dropped, and a template that wants the
    host's Docker is not a template we can honour — the whole stack goes.
  * More than four long-running services and the odds of getting startup order,
    shared state and init steps right without ever running it stop being
    acceptable. A broken Supabase is worse than no Supabase.
  * A one-shot service — creating a bucket, running a migration, generating a
    token — is not an app. It would be created, exit, and crash-loop forever.
    It is dropped and named in the notes, because the step is usually still
    needed and silently skipping it is how a template half-works.
"""
import base64, json, re, sys, yaml

DB_IMAGE = re.compile(r'\b(postgres|postgis|pgvector|mysql|mariadb|redis|valkey|keydb|mongo)\b', re.I)
ONESHOT = re.compile(r'(^|[-_])(init|migrat\w*|setup|bootstrap|seed|token-generator|schema-\w+|createbuckets?|install)([-_]|$)', re.I)
SOCKET = re.compile(r'/var/run/docker\.sock')
COOLIFY_VAR = re.compile(r'\$\{?SERVICE_|\$\{?COOLIFY')
DATASTORE_WORD = re.compile(
    r'(^|_)(DB|DATABASE|POSTGRES|POSTGRESQL|PG|MYSQL|MARIADB|REDIS|VALKEY|KEYDB|MONGO|MONGODB|'
    r'CACHE|QUEUE|BROKER|AMQP|RABBITMQ|ELASTIC|ELASTICSEARCH|MEILI|CLICKHOUSE)($|_)', re.I)
ADDRESS_WORD = re.compile(r'(^|_)(HOST|HOSTNAME|PORT|SERVER|ADDR|ADDRESS|URL|URI|DSN|CONNECTION|CONNECTIONSTRING)$', re.I)

ENGINE = [("postgres", ("postgres", "postgis", "pgvector")),
          ("mysql", ("mysql", "mariadb")),
          ("redis", ("redis", "valkey", "keydb"))]

MAX_SERVICES = 4


def slug(text):
    return re.sub(r'[^a-z0-9-]+', '-', text.lower()).strip("-")[:30]


def engine_of(image):
    for engine, words in ENGINE:
        if any(w in image.lower() for w in words):
            return engine
    return None


def port_of(body, fallback):
    """The port a service listens on, from the compose file's own words."""
    for entry in body.get("expose") or []:
        digits = re.sub(r'\D', '', str(entry))
        if digits:
            return int(digits)
    for entry in body.get("ports") or []:
        text = str(entry).split("/")[0]
        parts = text.split(":")
        if parts and parts[-1].isdigit():
            return int(parts[-1])
    for key, value in (body.get("environment") or {} if isinstance(body.get("environment"), dict)
                       else {k.split("=")[0]: k.split("=", 1)[-1] for k in (body.get("environment") or [])
                             if isinstance(k, str) and "=" in k}).items():
        if re.search(r'(^|_)PORT$', key, re.I) and str(value).strip("'\"").isdigit():
            return int(str(value).strip("'\""))
    match = re.search(r'SERVICE_FQDN_[A-Z0-9_]+_(\d+)', str(body.get("environment") or ""))
    if match:
        return int(match.group(1))
    return fallback


def env_of(body, databases):
    """Variables the app needs, minus the ones that point at a database.

    A reference to another *application* service is kept: those exist here, under
    the same name, reachable in the same namespace. A reference to a database is
    not: Skifity provisions those and injects a connection string instead.
    """
    out, raw = {}, body.get("environment")
    items = raw if isinstance(raw, list) else [f"{k}={v}" for k, v in (raw or {}).items()]
    for item in items or []:
        if not isinstance(item, str) or "=" not in item:
            continue
        key, value = item.split("=", 1)
        key, value = key.strip().strip("'\""), value.strip().strip("'\"")
        if not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', key):
            continue
        if COOLIFY_VAR.search(value) or "${" in value or "$" in value or value == "":
            continue
        if DATASTORE_WORD.search(key) and ADDRESS_WORD.search(key):
            continue
        if value in databases:
            continue
        out[key] = value
    return out


def volumes_of(body):
    out = []
    for entry in body.get("volumes") or []:
        if not isinstance(entry, str) or ":" not in entry:
            continue
        source, target = entry.split(":")[:2]
        if source.startswith(("/", ".")) or not target.startswith("/"):
            continue
        name = slug(source)
        if name and target:
            out.append({"name": name, "mount_path": target, "size_gb": 5})
    return out[:2]


def convert(key, template, compose, pinned):
    services = compose.get("services") or {}
    databases = {n: b for n, b in services.items()
                 if isinstance(b, dict) and DB_IMAGE.search(str(b.get("image", "")))
                 and engine_of(str(b.get("image", "")))}
    apps = [n for n in services if n not in databases]
    if len(apps) <= 1:
        return None, "not multi-service"

    if any(SOCKET.search(str(services[n].get("volumes") or "")) or services[n].get("privileged")
           for n in apps):
        return None, "needs the Docker socket or a privileged container"

    oneshot = [n for n in apps
               if ONESHOT.search(n) or str(services[n].get("restart", "")).lower() in ("no", '"no"')]
    real = [n for n in apps if n not in oneshot]
    if not 2 <= len(real) <= MAX_SERVICES:
        return None, f"{len(real)} long-running services"

    out_services, names = [], set()
    for name in real:
        body = services[name]
        image = pinned.get(f"{key}::{name}") or pinned.get(body.get("image", ""))
        if not image:
            return None, f"no verified image for {name}"
        port = port_of(body, template.get("port"))
        if not port or not 1 <= int(port) <= 65535:
            return None, f"no port for {name}"
        service_slug = slug(name) or slug(key)
        if service_slug in names:
            return None, "two services with the same slug"
        names.add(service_slug)

        entry = {
            "name": service_slug,
            "image": image,
            "port": int(port),
            # Public when the source marked it with a domain of its own. Each
            # app here can have a domain, so several public services is fine.
            "public": "SERVICE_FQDN" in str(body.get("environment") or ""),
            "mem_request_mb": 128, "mem_limit_mb": 1024,
            "cpu_request_m": 50, "cpu_limit_m": 1000,
        }
        variables = env_of(body, set(databases))
        if variables:
            entry["variables"] = variables
        volumes = volumes_of(body)
        if volumes:
            entry["volumes"] = volumes
        out_services.append(entry)

    if not any(s["public"] for s in out_services):
        # Nothing was marked, so the one carrying the template's own port is it.
        main = max(out_services, key=lambda s: s["port"] == template.get("port"))
        main["public"] = True

    out = {
        "id": slug(key),
        "name": template.get("name") or key.replace("-", " ").title(),
        "description": (template.get("slogan") or "").strip(),
        "category": template.get("category") or "other",
        "website": (template.get("documentation") or "").split("?")[0],
        "services": out_services,
    }

    if databases:
        first = list(databases)[0]
        out["databases"] = [{
            "name": (slug(key) + "-db")[:40],
            "engine": engine_of(str(databases[first].get("image", ""))),
            "storage_gb": 5,
            "link_to": out_services[0]["name"],
            "var_name": "DATABASE_URL",
        }]

    notes = []
    if oneshot:
        notes.append(
            "This stack has a set-up step that runs once and exits — "
            + ", ".join(sorted(oneshot))
            + ". Skifity runs apps, not one-shot containers, so that step is not "
            "installed: do it yourself from the app's Commands tab, or follow the "
            "project's own instructions, before expecting everything to work.")
    if len(out_services) > 1:
        notes.append(
            "This template installs "
            + str(len(out_services))
            + " apps. They reach each other by name inside the environment, so "
            "keep them together.")
    if notes:
        out["notes"] = " ".join(notes)
    return out, "ok"


if __name__ == "__main__":
    coolify = json.load(open("/tmp/tpl/coolify.json"))
    pinned = {}
    for source in ("/tmp/tpl/multi-resolved.json",):
        for key, value in json.load(open(source)).items():
            if value["to"]:
                pinned[key] = value["to"]
    for image, to in json.load(open("/tmp/tpl/already.json")).items():
        pinned[image] = to

    made, skipped = {}, {}
    for key, template in coolify.items():
        try:
            compose = yaml.safe_load(base64.b64decode(template["compose"]).decode())
        except Exception:
            continue
        out, why = convert(key, template, compose, pinned)
        if out:
            made[key] = out
        elif why != "not multi-service":
            skipped[key] = why

    json.dump(made, open("/tmp/tpl/converted-multi.json", "w"), indent=1)
    print(f"converted {len(made)}, skipped {len(skipped)}", file=sys.stderr)
    from collections import Counter
    for reason, count in Counter(skipped.values()).most_common(10):
        print(f"  {count:4}  {reason}", file=sys.stderr)
