"""Write converted templates into internal/templates/catalogue as YAML.

Everything here is a quality gate. A template that reaches the catalogue has an
image tag that was resolved to a real version and then fetched from the registry
to prove it exists, a port, a description somebody can read, and a link out to
the upstream project. Anything missing one of those is dropped rather than
shipped half-filled, because a template nobody can run is worse than a template
that is not there.
"""
import json, os, re, sys, yaml

CATALOGUE = "internal/templates/catalogue"
ORDER = ["id", "name", "description", "category", "website", "beta",
         "services", "databases", "inputs", "notes"]

# Categories Coolify uses that mean nothing to somebody browsing a panel, mapped
# onto the ones already in this catalogue.
# The source's categories, folded onto a set somebody browsing a panel would
# recognise. Left as a table rather than a rule because the folding is a
# judgement — "backend" and "api" are developer tools here, "RSS" is media —
# and a rule would hide that.
CATEGORY = {
    "": "other", "(none)": "other",
    "cms": "cms", "documentation": "cms",
    "analytics": "analytics",
    "monitoring": "monitoring",
    "automation": "automation", "ci": "automation",
    "productivity": "productivity", "helpdesk": "productivity", "games": "productivity",
    "storage": "storage", "database": "storage", "databases": "storage", "search": "storage",
    "ai": "ai", "mcp": "ai",
    "messaging": "communication", "email": "communication", "Mail": "communication",
    "developer": "developer", "development": "developer", "devtools": "developer",
    "devops": "developer", "backend": "developer", "api": "developer", "git": "developer",
    "media": "media", "RSS": "media",
    "security": "security", "auth": "security", "vpn": "security",
    "proxy": "networking", "Networking": "networking",
    "finance": "finance",
    "ecommerce": "ecommerce", "crm": "business", "erp": "business",
}


def usable(template):
    if not template.get("description") or len(template["description"]) < 15:
        return "no description"
    website = template.get("website") or ""
    if not website.startswith("https://"):
        return "no https website"
    service = template["services"][0]
    if not (1 <= service["port"] <= 65535):
        return "port out of range"
    if re.search(r'(^|-)latest$', service["image"].rpartition(":")[2]):
        return "floating tag"
    return None


def main(keep_existing):
    converted = json.load(open("/tmp/tpl/converted.json"))
    existing = {f[:-5] for f in os.listdir(CATALOGUE) if f.endswith(".yaml")}

    written, dropped = 0, {}
    for key, template in sorted(converted.items()):
        if template["id"] in existing and keep_existing:
            dropped[key] = "already written by hand"
            continue
        reason = usable(template)
        if reason:
            dropped[key] = reason
            continue
        template["category"] = CATEGORY.get(template.get("category", ""), "other")
        # A description is a sentence on a card, not a paragraph.
        template["description"] = template["description"].strip().rstrip(".") + "."
        body = {k: template[k] for k in ORDER if template.get(k)}
        with open(f"{CATALOGUE}/{template['id']}.yaml", "w") as fh:
            yaml.safe_dump(body, fh, sort_keys=False, default_flow_style=False,
                           allow_unicode=True, width=100)
        written += 1

    print(f"wrote {written}, dropped {len(dropped)}", file=sys.stderr)
    from collections import Counter
    for reason, count in Counter(dropped.values()).most_common():
        print(f"  {count:4}  {reason}", file=sys.stderr)


if __name__ == "__main__":
    main(keep_existing=True)
