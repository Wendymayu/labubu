"""CLI entry point: locate and execute the bundled Go binary."""
import os
import subprocess
import sys


def _get_binary_path():
    """Return the path to the bundled Go binary."""
    pkg_dir = os.path.dirname(os.path.abspath(__file__))
    binary = os.path.join(pkg_dir, "bin", "labubu")
    if sys.platform == "win32":
        binary += ".exe"
    return binary


def main():
    """Locate the Go binary and execute it with all CLI arguments."""
    binary = _get_binary_path()

    if not os.path.isfile(binary):
        print(
            f"Error: labubu binary not found at {binary}",
            file=sys.stderr,
        )
        print(
            "Try reinstalling: pip install --force-reinstall labubu",
            file=sys.stderr,
        )
        sys.exit(1)

    result = subprocess.run([binary] + sys.argv[1:])
    sys.exit(result.returncode)


def mcp_main():
    """Entry point for labubu-mcp command (MCP Server over stdio)."""
    from labubu.mcp.server import main as mcp_server_main
    mcp_server_main()
