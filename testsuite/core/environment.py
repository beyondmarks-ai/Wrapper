from core.wrap_manager import BaseWRAPManager
from core.fs_manager import TestFSManager

class Environment:
    """Manage test environment
    Manage cleanup of environment and other stuff at a single place
    """    
    def __init__(self, wrap_manager : BaseWRAPManager, fs_manager : TestFSManager ):
        self.wrap_mgr = wrap_manager
        self.fs_mgr = fs_manager

    def cleanup(self) -> None:
        self.wrap_mgr.close_wrap()
        self.fs_mgr.cleanup()