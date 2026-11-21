"""Credentials and personal data in what an agent sent its model: found, masked and redacted.

A session review (review.py) hands find() each piece of text the model was sent: what the person
typed, what the agent added, the arguments of its tool calls, what the tools returned and the system
prompt. find() returns what it recognised. A finding never carries the value it found: only the kind,
a masked preview (the prefix that says what kind of key it is, the last digits of a card) and its
length. sensitive_path() and dumps_environment() say whether a tool call read a file or the
environment that usually holds secrets, whatever came back.

redact() replaces every match in a text, for what the server passes on to a model of its own choosing
(the AI review's brief), so a review never sends on a secret the session leaked.

The detectors are patterns, not a scanner of record: they look for the common shapes (provider key
prefixes, private key blocks, passwords in URLs and assignments, emails, cards that pass Luhn, IBANs
that pass mod 97), and skip what looks like a placeholder or a documentation example. This module
imports nothing of the server's.
"""

from __future__ import annotations

import bisect
import math
import re
from collections import Counter
from dataclasses import dataclass
from typing import Callable

CREDENTIAL = "credential"
PII = "pii"
# A value this long or longer is checked; shorter ones are almost always placeholders or test values.
MIN_SECRET = 8
# Bits a character a guessed secret (an assignment, a .env line) must reach: "changeme123" doesn't, a real key does.
SECRET_ENTROPY = 3.0
# The same for a loose find (a thorough scan, and redaction): shorter, plainer values count too, at the cost of
# more false alarms. "hunter22" passes here, not above.
LOOSE_SECRET = 6
LOOSE_ENTROPY = 2.5
# Words that make a value a placeholder, an example or a test fixture rather than a real secret.
PLACEHOLDERS = re.compile(
    r"example|sample|dummy|fake|test|placeholder|changeme|change_me|your[_-]?|xxxx|\*\*\*|redacted|<[^>]*>|\$\{|\{\{|%\(|\.\.\.|…|"
    r"^(?:password|passwd|secret|token|admin|root|user|pass|postgres|guest|none|null|undefined|true|false)$",
    re.I,
)
EXAMPLE_DOMAINS = re.compile(r"(?:^|\.)(?:example\.(?:com|org|net)|test|invalid|localhost|local|example)$", re.I)
NO_PERSON = re.compile(r"^(?:git|noreply|no-reply|donotreply|do-not-reply|mailer-daemon|postmaster|root|admin|info|support|security|hello|contact)$", re.I)
# Things written like an email that are files or packages: logo@2x.png, pkg@1.2.3.
FILE_ENDINGS = {"png", "jpg", "jpeg", "gif", "svg", "webp", "ico", "js", "mjs", "cjs", "ts", "tsx", "jsx", "py", "go", "rs", "css", "scss", "json", "yaml", "yml",
                "md", "txt", "html", "htm", "xml", "lock", "toml", "sh", "ps1", "zip", "gz", "tar", "map", "vue", "svelte", "wasm"}
# Numbers that pass the card and IBAN checks but are the payment providers' own test values.
TEST_NUMBERS = {
    "4242424242424242", "4111111111111111", "4000056655665556", "5555555555554444", "5105105105105100", "2223003122003222", "378282246310005",
    "371449635398431", "6011111111111117", "6011000990139424", "3056930009020004", "4012888888881881", "GB82WEST12345698765432", "DE89370400440532013000",
}


@dataclass(frozen=True)
class Detector:
    id: str
    label: str
    kind: str
    severity: str
    pattern: re.Pattern[str]
    # The pattern's group that holds the value: 0 for the whole match.
    group: int = 0
    check: Callable[[str], bool] | None = None
    mask: Callable[[str], str] | None = None


@dataclass(frozen=True)
class Found:
    detector: Detector
    value: str


def entropy(value: str) -> float:
    """Shannon entropy in bits a character: how random a string looks."""
    counts = Counter(value)
    return -sum(count / len(value) * math.log2(count / len(value)) for count in counts.values()) if value else 0.0


def _real(value: str) -> bool:
    return not PLACEHOLDERS.search(value) and len(set(value)) > 3


def _random(value: str, shortest: int = MIN_SECRET, bits: float = SECRET_ENTROPY) -> bool:
    """A guessed secret: long enough, random enough, and not a word, an identifier or code."""
    if len(value) < shortest or not _real(value) or re.search(r"[(){}\[\]<>]", value) or value.startswith(("$", "%")):
        # Code, or a variable it's read from ($DB_PASS, %APPDATA%).
        return False
    if re.fullmatch(r"[a-z_.-]+|[A-Z_.-]+|[\w.-]*\.[a-z_]\w*", value) or value.startswith(("http", "/", "./", "~")):
        # A plain word or identifier (settings.api_key, os.environ, PASSWORD_FILE), or a path or URL.
        return False
    return entropy(value) >= bits


def _loose(value: str) -> bool:
    return _random(value, LOOSE_SECRET, LOOSE_ENTROPY)


def _keyish(value: str) -> bool:
    return _real(value) and bool(re.search(r"\d", value)) and entropy(value) >= 3.5


def _luhn(digits: str) -> bool:
    total = 0
    for index, char in enumerate(reversed(digits)):
        number = int(char) * (2 if index % 2 else 1)
        total += number - 9 if number > 9 else number
    return total % 10 == 0


def _card(value: str) -> bool:
    digits = re.sub(r"\D", "", value)
    if digits in TEST_NUMBERS or len(set(digits)) < 4 or not _luhn(digits):
        return False
    # A known card network's numbers and length: anything else that passes Luhn is one number in ten.
    return bool(
        (len(digits) == 16 and re.match(r"4|5[1-5]|2(?:2[2-9]|[3-6]\d|7[01]|720)|6(?:011|5)", digits))
        or (len(digits) == 15 and re.match(r"3[47]", digits))
    )


def _iban(value: str) -> bool:
    compact = value.replace(" ", "")
    if compact in TEST_NUMBERS or not 15 <= len(compact) <= 34:
        return False
    moved = compact[4:] + compact[:4]
    return int("".join(str(int(char, 36)) for char in moved)) % 97 == 1


def _email(value: str) -> bool:
    local, _, domain = value.rpartition("@")
    ending = domain.rsplit(".", 1)[-1].lower()
    return bool(local) and not NO_PERSON.match(local) and not EXAMPLE_DOMAINS.search(domain) and ending not in FILE_ENDINGS \
        and not domain.lower().endswith("noreply.github.com") and not re.fullmatch(r"[\d.]+", local)


def _phone(value: str) -> bool:
    digits = re.sub(r"\D", "", value)
    return 10 <= len(digits) <= 15 and len(set(digits)) > 3


def _prefix(size: int) -> Callable[[str], str]:
    return lambda value: f"{value[:size]}… ({len(value)} characters)"


def _hidden(value: str) -> str:
    return f"… ({len(value)} characters)"


def _last(size: int, fill: str) -> Callable[[str], str]:
    return lambda value: fill + re.sub(r"\W", "", value)[-size:]


def _masked_email(value: str) -> str:
    local, _, domain = value.rpartition("@")
    return f"{local[0]}•••@{domain}"


def _d(id: str, label: str, kind: str, severity: str, pattern: str, group: int = 0, check: Callable[[str], bool] | None = None,
       mask: Callable[[str], str] | None = None, flags: int = 0) -> Detector:
    return Detector(id, label, kind, severity, re.compile(pattern, flags), group, check, mask)


# Most specific first: a value one detector took isn't counted again by a later one.
DETECTORS: tuple[Detector, ...] = (
    _d("private-key", "Private key", CREDENTIAL, "high",
       r"-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----(?:\s|\\n|\\r)*(?:[A-Za-z-]+:[^\n]*(?:\s|\\n)+)*[A-Za-z0-9+/=]{40,}",
       mask=lambda value: "-----BEGIN … PRIVATE KEY-----"),
    _d("aws-access-key", "AWS access key", CREDENTIAL, "high", r"\b((?:AKIA|ASIA)[0-9A-Z]{16})\b", 1, _real, _prefix(4)),
    _d("aws-secret-key", "AWS secret key", CREDENTIAL, "high", r"(?i)aws_?secret_?access_?key[\"']?\s*[:=]\s*[\"']?([A-Za-z0-9/+=]{40})(?![A-Za-z0-9/+=])", 1, _real, _hidden),
    _d("github-token", "GitHub token", CREDENTIAL, "high", r"\b((?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{22,255})\b", 1, _real, _prefix(4)),
    _d("gitlab-token", "GitLab token", CREDENTIAL, "high", r"\b(glpat-[A-Za-z0-9_-]{20,})", 1, _real, _prefix(6)),
    _d("slack-token", "Slack token", CREDENTIAL, "high", r"\b(xox[abposr]-[A-Za-z0-9-]{10,})", 1, _real, _prefix(5)),
    _d("slack-webhook", "Slack webhook", CREDENTIAL, "high", r"(https://hooks\.slack\.com/services/T[A-Za-z0-9_]+/B[A-Za-z0-9_]+/[A-Za-z0-9_]+)", 1, _real,
       lambda value: "https://hooks.slack.com/services/…"),
    _d("anthropic-key", "Anthropic API key", CREDENTIAL, "high", r"\b(sk-ant-[A-Za-z0-9_-]{20,})", 1, _real, _prefix(7)),
    _d("openai-key", "OpenAI-style API key", CREDENTIAL, "high", r"\b(sk-(?!ant-)(?:proj-|svcacct-|admin-)?[A-Za-z0-9_-]{20,})", 1, _keyish, _prefix(3)),
    _d("google-api-key", "Google API key", CREDENTIAL, "high", r"\b(AIza[0-9A-Za-z_-]{35})", 1, _real, _prefix(4)),
    _d("stripe-key", "Stripe live key", CREDENTIAL, "high", r"\b((?:sk|rk)_live_[0-9A-Za-z]{20,})", 1, _real, _prefix(8)),
    _d("npm-token", "npm token", CREDENTIAL, "high", r"\b(npm_[A-Za-z0-9]{36})\b", 1, _real, _prefix(4)),
    _d("huggingface-token", "Hugging Face token", CREDENTIAL, "high", r"\b(hf_[A-Za-z0-9]{34,})\b", 1, _real, _prefix(3)),
    _d("jwt", "JSON web token", CREDENTIAL, "medium", r"\b(eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})", 1, None, _prefix(3)),
    _d("url-password", "Password in a URL", CREDENTIAL, "medium",
       r"\b[a-zA-Z][a-zA-Z0-9+.-]{1,20}://[^\s:/@\"'<>]{1,64}:([^\s@/\"'<>]{8,128})@[^\s/\"'<>]+", 1, _random, _hidden),
    _d("auth-header", "Authorization header", CREDENTIAL, "medium",
       r"(?i)\bauthorization[\"']?\s*[:=]\s*[\"']?(?:bearer|token|basic)\s+([A-Za-z0-9._~+/=-]{16,})", 1, _real, _hidden),
    _d("secret-assignment", "Password or secret in code or config", CREDENTIAL, "medium",
       r"(?i)(?:password|passwd|secret|api[_-]?key|access[_-]?key|access[_-]?token|auth[_-]?token|private[_-]?key)[\w-]*[\"']?\s*[:=]\s*[\"']([^\"'\s]{8,200})[\"']",
       1, _random, _hidden),
    _d("secret-env", "Secret in an environment file", CREDENTIAL, "medium",
       r"(?m)^[ \t]*(?:export[ \t]+)?[A-Z][A-Z0-9_]*(?:PASSWORD|PASSWD|SECRET|TOKEN|API_KEY|APIKEY|ACCESS_KEY|PRIVATE_KEY)[A-Z0-9_]*[ \t]*=[ \t]*([^\s\"'#$]{8,200})",
       1, _random, _hidden),
    _d("card-number", "Payment card number", PII, "high", r"(?<![\d-])((?:\d[ -]?){14,15}\d)(?![\d-])", 1, _card, _last(4, "•••• ")),
    _d("us-ssn", "US social security number", PII, "high", r"\b((?!000|666|9\d\d)\d{3}-(?!00)\d{2}-(?!0000)\d{4})\b", 1, None, _last(4, "•••-••-")),
    _d("iban", "Bank account (IBAN)", PII, "medium", r"\b([A-Z]{2}\d{2}(?: ?[A-Z0-9]{4}){2,7}(?: ?[A-Z0-9]{1,4})?)\b", 1, _iban,
       lambda value: value[:2] + "•• •••• " + value.replace(" ", "")[-4:]),
    _d("email", "Email address", PII, "low", r"\b([A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,24})\b", 1, _email, _masked_email),
    _d("phone", "Phone number", PII, "low", r"(?<![\w+])(\+[1-9][0-9 ().-]{8,18}[0-9])(?![\w])", 1, _phone, _last(2, "+•• ••• ••")),
)
BY_ID = {detector.id: detector for detector in DETECTORS}


def find(text: str, kinds: frozenset[str] | set[str] | None = None, loose: bool = False) -> list[Found]:
    """Everything the detectors recognise in text, each value once, in the order of DETECTORS. kinds keeps to
    those kinds (CREDENTIAL, PII); loose takes guessed secrets that are shorter and plainer (_loose)."""
    found: list[Found] = []
    # The spans earlier detectors took, sorted and apart: a dump of thousands of addresses stays linear-ish.
    starts: list[int] = []
    ends: list[int] = []
    seen: set[tuple[str, str]] = set()
    for detector in DETECTORS:
        if kinds is not None and detector.kind not in kinds:
            continue
        check = _loose if loose and detector.check is _random else detector.check
        for match in detector.pattern.finditer(text):
            value = match.group(detector.group)
            start, end = match.span(detector.group)
            index = bisect.bisect_right(starts, start)
            if (index and ends[index - 1] > start) or (index < len(starts) and starts[index] < end):
                continue
            if check and not check(value):
                continue
            starts.insert(index, start)
            ends.insert(index, end)
            if (detector.id, value) not in seen:
                seen.add((detector.id, value))
                found.append(Found(detector, value))
    return found


def masked(item: Found) -> str:
    """What a finding shows of its value: never the value itself."""
    return item.detector.mask(item.value) if item.detector.mask else _hidden(item.value)


def redact(text: str) -> str:
    """text with every credential and piece of personal data the detectors find replaced by its label. It finds
    loosely whatever the scan's level: removing too much from a brief costs nothing, too little leaks."""
    for item in find(text, loose=True):
        text = text.replace(item.value, f"[{item.detector.label.lower()} removed]")
    return text


# Files that hold secrets by their nature: what comes back from reading one is in the model's context.
SENSITIVE_FILE = re.compile(
    r"(?:^|[\\/])(?:"
    r"\.env(?:\.(?!example\b|sample\b|template\b|dist\b|defaults?\b)[\w.-]+)?|\.envrc|"
    r"id_(?:rsa|dsa|ecdsa|ed25519)|[\w.-]+\.(?:pem|key|p12|pfx|jks|keystore|kdbx|ppk|asc)|"
    r"\.netrc|_netrc|\.npmrc|\.pypirc|\.git-credentials|\.pgpass|\.htpasswd|"
    r"credentials(?:\.json|\.ya?ml)?|service[-_]?account[\w.-]*\.json|secrets?\.(?:ya?ml|json|toml|env)|"
    r"kubeconfig|terraform\.tfstate(?:\.backup)?|"
    r"\.aws[\\/](?:credentials|config)|\.kube[\\/]config|\.docker[\\/]config\.json|\.config[\\/]gh[\\/]hosts\.yml"
    r")$",
    re.I,
)
# Words in a shell command that are paths: what a command like `cat ~/.aws/credentials | head` reads.
PATH_WORD = re.compile(r"[^\s|;&<>\"'`()]+")
# Commands that print every environment variable, secrets and all.
ENVIRONMENT_DUMP = re.compile(r"^\s*(?:env|printenv|export -p|set|declare -x|(?:get-childitem|gci|ls|dir) env:)\s*(?:$|[|>;&])", re.I)


def sensitive_path(text: str) -> str | None:
    """The first path in text (a file argument or a shell command) that names a file of secrets."""
    for word in PATH_WORD.findall(text or ""):
        word = word.rstrip(",.:")
        if word.endswith(".pub"):
            continue
        if SENSITIVE_FILE.search(word):
            return word
    return None


def dumps_environment(command: str) -> bool:
    return any(ENVIRONMENT_DUMP.match(part) for part in re.split(r"&&|\|\||;", command or ""))
