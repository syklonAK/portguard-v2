"""Validate all PortGuard ansible YAML files parse correctly."""
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("PyYAML missing - installing")
    import subprocess
    subprocess.check_call([sys.executable, "-m", "pip", "install", "PyYAML", "-q"])
    import yaml

root = Path("ansible")
files = list(root.rglob("*.yml")) + list(root.rglob("*.yaml")) + [root / "ansible.cfg"]
failed = 0
for f in files:
    if f.suffix == ".cfg" or f.name == "hosts.ini":
        continue
    try:
        data = yaml.safe_load(f.read_text(encoding="utf-8"))
        if f.name.startswith(("site", "service")) and not isinstance(data, list):
            print(f"FAIL {f}: playbook must be a list of plays")
            failed += 1
        else:
            print(f"OK   {f}")
    except yaml.YAMLError as e:
        print(f"FAIL {f}: {e}")
        failed += 1

# basic structural checks on playbooks
site = yaml.safe_load((root / "site.yml").read_text())
assert isinstance(site, list) and len(site) == 2, "site.yml: expected 2 plays"
plays = {p["name"]: p for p in site}
assert plays["PortGuard control plane (master)"]["hosts"] == "portguard_master"
assert "portguard_panel" in plays["PortGuard control plane (master)"]["roles"]
assert plays["PortGuard managed nodes (agents)"]["hosts"] == "portguard_nodes"
print("OK   site.yml structure (2 plays, roles wired)")

svc = yaml.safe_load((root / "service.yml").read_text())
assert isinstance(svc, list) and svc[0]["hosts"] == "portguard_master"
assert "portguard_service" in svc[0]["roles"]
print("OK   service.yml structure")

# role task files parse and contain expected keys
for role, must_contain in [
    ("portguard_panel", ["Install required packages", "Install systemd unit", "Wait for the panel to answer"]),
    ("portguard_node", ["Download the agent binary from the master", "Install the agent systemd unit", "Wait for the agent to answer"]),
    ("portguard_service", ["Login to the panel API"]),
]:
    p = root / "roles" / role / "tasks" / "main.yml"
    text = p.read_text(encoding="utf-8")
    for needle in must_contain:
        assert needle in text, f"{role}: missing task '{needle}'"
    print(f"OK   role {role}: required tasks present")

sys.exit(1 if failed else 0)
