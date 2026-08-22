import libtmux
import shlex
import time
import logging
import core.keys as keys
from core.wrap_manager import BaseWRAPManager

class TmuxWRAPManager(BaseWRAPManager):
    """
    Tmux based Manager
    After running wrap, you can connect to the session via
    tmux -L wrapper attach -t wrap_session
    Wont work in windows
    """
    # Class variables
    WRAP_START_DELAY : float = 0.1 # seconds
    WRAP_SOCKET_NAME : str = "wrapper"

    # Init should not allocate any resources
    def __init__(self, wrap_path : str):
        super().__init__(wrap_path)

        # Check libtmux version requirement
        min_version = (0, 31, 0)
        current_version_str = libtmux.__version__

        # Parse version string to tuple for comparison
        try:
            current_version = tuple(map(int, current_version_str.split('.')[:3]))
        except (ValueError, AttributeError):
            current_version = (0, 0, 0)

        if current_version < min_version:
            raise RuntimeError(
                f"libtmux version 0.31.0 or higher is required. "
                f"Current version: {current_version_str}. "
                f"Please upgrade with: pip install 'libtmux>=0.31.0'"
            )

        self.logger = logging.getLogger()
        self.server = libtmux.Server(socket_name=TmuxWRAPManager.WRAP_SOCKET_NAME)
        self.logger.debug("server object : %s", self.server)
        self.wrap_session : libtmux.Session = None
        self.wrap_pane : libtmux.Pane = None

    def start_wrap(self, start_dir : str = None, args : list[str] = None) -> None:
        command_parts = ["env", "WRAPPER_SKIP_WELCOME=1", self.wrap_path]
        if args:
            command_parts.extend(args)
        wrap_command = shlex.join(command_parts)

        self.logger.debug("windows_command : %s", wrap_command)


        self.wrap_session= self.server.new_session('wrap_session',
                window_command=wrap_command,
                start_directory=start_dir)
        time.sleep(TmuxWRAPManager.WRAP_START_DELAY)
        self.logger.debug("wrap_session initialised : %s", self.wrap_session)

        # If libtmux version is less than 0.3.1, active_pane does not exist.
        self.wrap_pane = self.wrap_session.active_pane
        self._is_wrap_running = True

    def _send_key(self, key : str) -> None:
        self.logger.debug("sending key : %s", repr(key))
        self.wrap_pane.send_keys(key, enter=False)

    def send_text_input(self, text : str, all_at_once : bool = True) -> None:
        if all_at_once:
            self._send_key(text)
        else:
            for c in text:
                self._send_key(c)

    def send_special_input(self, key : keys.Keys) -> str:
        if key.ascii_code != keys.NO_ASCII:
            self._send_key(chr(key.ascii_code))
        elif isinstance(key, keys.SpecialKeys):
            self._send_key(key.key_name)
        else:
            raise Exception(f"Unknown key : {key}")

    def get_rendered_output(self) -> str:
        return "[Not supported yet]"

    def is_wrap_running(self) -> bool:
        self._is_wrap_running = (self.wrap_session is not None) \
            and (self.wrap_session in self.server.sessions)

        return self._is_wrap_running

    def close_wrap(self) -> None:
        if self.is_wrap_running():
            self.server.kill_session(self.wrap_session.name)

    # Override
    def runtime_info(self) -> str:
        return str(self.server.sessions)

    def __repr__(self) -> str:
        return f"{self.__class__.__name__}(server : {self.server}, " + \
            f"session : {self.wrap_session}, running : {self._is_wrap_running})"
