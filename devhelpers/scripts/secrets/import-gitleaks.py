"""Writes server/agent_server/exposure_rules.py from a gitleaks release's rule set (gitleaks is MIT licensed).

The session review's scanner (exposure.py) runs these after its own detectors. gitleaks writes its
patterns for Go's RE2; this converts the few spellings Python reads differently (a flag group partway
through a pattern, \\z) and drops the rules that need a file path, which a model's traffic doesn't
have. Run it again to take a newer release, then run the server's tests:

    python devhelpers/scripts/secrets/import-gitleaks.py v8.30.1
    python devhelpers/scripts/secrets/import-gitleaks.py v8.30.1 --from gitleaks.toml   # a file already downloaded
"""

from __future__ import annotations

import argparse
import re
import tomllib
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
OUT = ROOT / "server" / "agent_server" / "exposure_rules.py"
SOURCE = "https://raw.githubusercontent.com/gitleaks/gitleaks/{version}/{path}"

# Rules exposure.py's own detectors already cover (with a better preview), the catch-all exposure.py has its own
# version of, and rules that only apply to a file of a given name.
SKIPPED = {
    "private-key", "jwt", "aws-access-token", "github-pat", "github-oauth", "github-app-token", "github-refresh-token",
    "github-fine-grained-pat", "gitlab-pat", "slack-webhook-url", "anthropic-api-key", "openai-api-key", "gcp-api-key",
    "stripe-access-token", "npm-access-token", "huggingface-access-token", "generic-api-key",
}
WORDS = {
    "api": "API", "id": "ID", "aws": "AWS", "gcp": "GCP", "jwt": "JWT", "pat": "personal access token", "oauth": "OAuth",
    "ca": "CA", "ci": "CI", "cicd": "CI/CD", "ad": "AD", "scim": "SCIM", "ptt": "pipeline trigger token", "rrt": "runner registration token",
    "url": "URL", "yaml": "YAML", "tf": "Terraform", "pypi": "PyPI", "npm": "npm", "flyio": "Fly.io", "frameio": "Frame.io",
    "1password": "1Password", "github": "GitHub", "gitlab": "GitLab", "digitalocean": "DigitalOcean", "openai": "OpenAI",
    "launchdarkly": "LaunchDarkly", "hubspot": "HubSpot", "jfrog": "JFrog", "mailchimp": "Mailchimp", "messagebird": "MessageBird",
    "planetscale": "PlanetScale", "sendgrid": "SendGrid", "sumologic": "Sumo Logic", "travisci": "Travis CI", "droneci": "Drone CI",
    "nytimes": "New York Times", "rubygems": "RubyGems", "clickhouse": "ClickHouse", "privateai": "Private AI", "rapidapi": "RapidAPI",
    "settlemint": "SettleMint", "sourcegraph": "Sourcegraph", "linkedin": "LinkedIn", "youtube": "YouTube", "intra42": "42 Intra",
    "base64": "(base64)", "personnal": "personal", "v2": "(v2)", "amazon": "Amazon",
}
# A rule for something that isn't a secret on its own (a client or account ID) or only works against a test system.
LOWER = re.compile(r"(?:^|-)(?:id|client-id|access-id|api-id|pub|public|test)(?:-|$)")


def python_regex(pattern: str) -> str:
    """pattern, written for RE2, as Python reads it the same way. RE2 lets a flag group like (?i) sit anywhere and
    apply to the rest of its group; Python only takes one at the start, so a later one becomes a scoped (?i:...)."""
    pattern = pattern.replace(r"\z", r"\Z")
    out: list[str] = []
    # For each open group, the scoped flag groups opened in it: closed when it closes, and closed and opened again
    # around each | in it, since RE2's flags carry across the alternatives while a Python group would swallow them.
    closes: list[list[str]] = [[]]
    index, in_class = 0, False
    while index < len(pattern):
        char = pattern[index]
        if char == "\\":
            out.append(pattern[index:index + 2])
            index += 2
            continue
        if in_class:
            if char == "]":
                in_class = False
            # RE2 reads [ in a class as itself; Python warns it may one day mean a nested set.
            out.append("\\[" if char == "[" else char)
            index += 1
            continue
        if char == "[":
            in_class = True
            negated = pattern.startswith("[^", index)
            out.append("[^" if negated else "[")
            index += 2 if negated else 1
            if pattern.startswith("]", index):
                out.append("\\]")
                index += 1
            continue
        flags = re.match(r"\(\?([imsU-]+)\)", pattern[index:])
        if flags and (index or len(closes) > 1):
            out.append(f"(?{flags.group(1)}:")
            closes[-1].append(flags.group(1))
            index += len(flags.group(0))
            continue
        if char == "(":
            closes.append([])
        elif char == ")":
            out.append(")" * len(closes.pop()))
        elif char == "|" and closes[-1]:
            out.append(")" * len(closes[-1]) + "|" + "".join(f"(?{flag}:" for flag in closes[-1]))
            index += 1
            continue
        out.append(char)
        index += 1
    out.append(")" * len(closes.pop()))
    return "".join(out)


def label(rule_id: str) -> str:
    words = [WORDS.get(word, word) for word in rule_id.split("-")]
    text = " ".join(words)
    return text[0].upper() + text[1:]


def rules(config: dict) -> list[tuple]:
    out = []
    for rule in config["rules"]:
        if rule["id"] in SKIPPED or "regex" not in rule or "path" in rule:
            continue
        pattern = python_regex(rule["regex"])
        re.compile(pattern)
        allowed, allowed_match, stopwords = [], [], []
        for allow in rule.get("allowlists", []):
            if allow.get("paths") and allow.get("condition") == "AND":
                continue
            target = allow.get("regexTarget", "secret")
            if target == "secret":
                allowed += [python_regex(item) for item in allow.get("regexes", [])]
            elif target == "match":
                allowed_match += [python_regex(item) for item in allow.get("regexes", [])]
            stopwords += allow.get("stopwords", [])
        out.append((
            rule["id"], label(rule["id"]), "medium" if LOWER.search(rule["id"]) else "high", pattern,
            rule.get("secretGroup", -1), float(rule.get("entropy", 0)), tuple(word.lower() for word in rule.get("keywords", [])),
            tuple(allowed), tuple(allowed_match), tuple(stopwords),
        ))
    return out


def fetch(version: str, path: str) -> str:
    with urllib.request.urlopen(SOURCE.format(version=version, path=path), timeout=60) as response:
        return response.read().decode()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("version", help="a gitleaks release tag, like v8.30.1")
    parser.add_argument("--from", dest="source", type=Path, help="a gitleaks.toml already downloaded from that release")
    parser.add_argument("--license", type=Path, help="its LICENSE, already downloaded")
    args = parser.parse_args()
    text = args.source.read_text() if args.source else fetch(args.version, "config/gitleaks.toml")
    licence = (args.license.read_text() if args.license else fetch(args.version, "LICENSE")).strip()
    config = tomllib.loads(text)
    found = rules(config)
    allow = config.get("allowlist", {})
    lines = [
        f'"""Credential patterns from gitleaks {args.version} (https://github.com/gitleaks/gitleaks), for exposure.py.',
        "",
        "Written by devhelpers/scripts/secrets/import-gitleaks.py: run it again rather than editing this file.",
        "The patterns are gitleaks', converted to Python's regular expressions. gitleaks' licence:",
        "",
        *licence.splitlines(),
        '"""',
        "",
        "# fmt: off",
        "from __future__ import annotations",
        "",
        f"VERSION = {args.version!r}",
        "# Values no rule counts: variables, template placeholders, paths and runs of one letter.",
        f"ALLOWED = {tuple(python_regex(item) for item in allow.get('regexes', []))!r}",
        f"STOPWORDS = {tuple(allow.get('stopwords', []))!r}",
        "# id, label, severity, pattern, the group holding the secret (-1: the first that matched, else the whole match),",
        "# the entropy it must reach, keywords one of which the text must hold (lower case), secrets allowed (patterns),",
        "# matches allowed (patterns), words that make a secret not one.",
        "RULES: tuple[tuple[str, str, str, str, int, float, tuple[str, ...], tuple[str, ...], tuple[str, ...], tuple[str, ...]], ...] = (",
        *(f"    {rule!r}," for rule in found),
        ")",
        "",
    ]
    OUT.write_text("\n".join(lines), newline="\n")
    print(f"{len(found)} rules from gitleaks {args.version} into {OUT.relative_to(ROOT)}")


if __name__ == "__main__":
    main()
