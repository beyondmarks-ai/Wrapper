import time
import subprocess
import pyautogui
import core.keys as keys
from core.wrap_manager import BaseWRAPManager

class PyAutoGuiWRAPManager(BaseWRAPManager):
    """Manage WRAP via subprocesses and pyautogui
    Cross platform, but it globally takes over the input, so you need the terminal
    constantly on focus during test run
    """
    WRAP_START_DELAY : float = 0.5
    def __init__(self, wrap_path : str):
        super().__init__(wrap_path)
        self.wrap_process = None


    def start_wrap(self, start_dir : str = None, args : list[str] = None) -> None:
        wrap_args = [self.wrap_path]
        if args :
            wrap_args += args
        wrap_args.append(start_dir)

        self.wrap_process = subprocess.Popen(wrap_args,
            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        time.sleep(PyAutoGuiWRAPManager.WRAP_START_DELAY)

        # Need to send a sample keypress otherwise it ignores first keypress
        self.send_text_input('x')


    def send_text_input(self, text : str, all_at_once : bool = False) -> None:
        if all_at_once :
            pyautogui.write(text)
        else:
            for c in text:
                pyautogui.write(c)

    def send_special_input(self, key : keys.Keys) -> None:
        if isinstance(key, keys.CtrlKeys):
            pyautogui.hotkey('ctrl', key.char)
        elif isinstance(key, keys.SpecialKeys):
            pyautogui.press(key.key_name.lower())
        else:
            raise Exception(f"Unknown key : {key}")

    def get_rendered_output(self) -> str:
        return "[Not supported yet]"


    def is_wrap_running(self) -> bool:
        self._is_wrap_running = (self.wrap_process is not None) and (self.wrap_process.poll() is None)
        return self._is_wrap_running

    def close_wrap(self) -> None:
        if self.wrap_process is not None:
            self.wrap_process.terminate()

    # Override
    def runtime_info(self) -> str:
        if self.wrap_process is None:
            return "[No process]"
        else:
            return f"[PID : {self.wrap_process.pid}, poll : {self.wrap_process.poll()}]"



