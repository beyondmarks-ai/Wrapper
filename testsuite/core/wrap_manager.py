from abc import ABC, abstractmethod
import core.keys as keys

class BaseWRAPManager(ABC):

    def __init__(self, wrap_path : str):
        self.wrap_path = wrap_path
        # _ denotes the internal variables, anyone should not directly read/modify
        self._is_wrap_running : bool = False

    @abstractmethod
    def start_wrap(self, start_dir : str = None, args : list[str] = None) -> None:
        pass 
    
    @abstractmethod
    def send_text_input(self, text : str, all_at_once : bool = False) -> None:
        pass 

    @abstractmethod
    def send_special_input(self, key : keys.Keys) -> None:
        pass 

    @abstractmethod
    def get_rendered_output(self) -> str:
        pass
    
    
    @abstractmethod
    def is_wrap_running(self) -> bool:
        """
        We allow using _is_wrap_running variable for efficiency
        But this method should give the true state, although this might have some calculations
        """
        return self._is_wrap_running
    
    @abstractmethod
    def close_wrap(self) -> None:
        """
        Close wrap if its running and cleanup any other resources
        """
    
    def runtime_info(self) -> str:
        return "[No runtime info]"

