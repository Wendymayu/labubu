import os
import subprocess
import sys
import unittest
from unittest.mock import patch, MagicMock

from labubu import cli


class TestGetBinaryPath(unittest.TestCase):
    """Test binary path resolution."""

    def test_unix_path(self):
        """On non-Windows, binary has no extension."""
        with patch.object(cli.sys, "platform", "linux"):
            path = cli._get_binary_path()
            self.assertTrue(path.endswith(os.path.join("bin", "labubu")))
            self.assertFalse(path.endswith(".exe"))

    def test_windows_path(self):
        """On Windows, binary has .exe extension."""
        with patch.object(cli.sys, "platform", "win32"):
            path = cli._get_binary_path()
            self.assertTrue(path.endswith(os.path.join("bin", "labubu.exe")))

    def test_path_is_absolute(self):
        """Binary path should be absolute."""
        path = cli._get_binary_path()
        self.assertTrue(os.path.isabs(path))


class TestMain(unittest.TestCase):
    """Test the main() CLI entry point."""

    @patch("labubu.cli.subprocess.run")
    @patch("labubu.cli.os.path.isfile", return_value=True)
    def test_passes_all_args_to_binary(self, mock_isfile, mock_run):
        """All CLI arguments after the program name are forwarded."""
        mock_run.return_value = MagicMock(returncode=0)

        with patch.object(cli.sys, "argv", ["labubu", "serve", "--port", "9090"]):
            with self.assertRaises(SystemExit):
                cli.main()

        mock_run.assert_called_once()
        call_args = mock_run.call_args[0][0]
        # First arg is the binary path, rest are forwarded args.
        self.assertEqual(call_args[1:], ["serve", "--port", "9090"])

    @patch("labubu.cli.subprocess.run")
    @patch("labubu.cli.os.path.isfile", return_value=True)
    def test_exits_with_binary_return_code(self, mock_isfile, mock_run):
        """main() exits with the same return code as the Go binary."""
        mock_run.return_value = MagicMock(returncode=42)

        with patch.object(cli.sys, "argv", ["labubu", "version"]):
            with self.assertRaises(SystemExit) as cm:
                cli.main()
            self.assertEqual(cm.exception.code, 42)

    @patch("labubu.cli.subprocess.run")
    @patch("labubu.cli.os.path.isfile", return_value=True)
    def test_exits_zero_on_success(self, mock_isfile, mock_run):
        """main() exits with 0 when the Go binary succeeds."""
        mock_run.return_value = MagicMock(returncode=0)

        with patch.object(cli.sys, "argv", ["labubu", "help"]):
            with self.assertRaises(SystemExit) as cm:
                cli.main()
            self.assertEqual(cm.exception.code, 0)

    @patch("labubu.cli.os.path.isfile", return_value=False)
    def test_binary_not_found_exits_with_1(self, mock_isfile):
        """main() exits with 1 and prints error when binary is missing."""
        with patch.object(cli.sys, "argv", ["labubu", "serve"]):
            with self.assertRaises(SystemExit) as cm:
                cli.main()
            self.assertEqual(cm.exception.code, 1)

    @patch("labubu.cli.subprocess.run", side_effect=KeyboardInterrupt)
    @patch("labubu.cli.os.path.isfile", return_value=True)
    def test_keyboard_interrupt_propagates(self, mock_isfile, mock_run):
        """Ctrl+C (KeyboardInterrupt) propagates to the caller."""
        with patch.object(cli.sys, "argv", ["labubu", "serve"]):
            with self.assertRaises(KeyboardInterrupt):
                cli.main()


if __name__ == "__main__":
    unittest.main()
