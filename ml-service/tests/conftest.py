import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))

# Generate the gRPC stubs from the shared .proto, like the Dockerfile does. This must
# happen at import time: test modules import app.grpc_server during collection, which
# is before any fixture runs.
subprocess.run([sys.executable, str(ROOT / "scripts" / "gen_proto.py")], check=True)
