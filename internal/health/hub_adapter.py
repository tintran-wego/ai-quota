"""Use maintained Hub probes with bounded parallelism and incremental results."""
import concurrent.futures
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
sys.path.insert(0, str(root / "scripts"))
import preflight

# AIQuota inventories hosts separately. Avoid a second slow/flapping MCP sweep.
# None is canonical "cannot verify", never "registered" or "healthy".
preflight.probes.mcp_server_names = lambda: None
config = preflight.load_config()
manifest = preflight.derive_manifest(json.loads(sys.argv[2]), preflight.repo_profile, None)
prober = preflight.make_prober(config)


def check(row):
    try:
        status = prober(row["id"])["status"]
    except Exception:
        status = "unknown"
    return {"id": row["id"], "status": status}


with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    for result in concurrent.futures.as_completed([pool.submit(check, row) for row in manifest]):
        print(json.dumps(result.result()), flush=True)
