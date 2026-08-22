wrap() {
    os=$(uname -s)

    # Linux
    if [[ "$os" == "Linux" ]]; then
        export WRAP_LAST_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/wrapper/lastdir"
    fi

    # macOS
    if [[ "$os" == "Darwin" ]]; then
        export WRAP_LAST_DIR="$HOME/Library/Application Support/wrapper/lastdir"
    fi

    command wrap "$@"

    [ ! -f "$WRAP_LAST_DIR" ] || {
        . "$WRAP_LAST_DIR"
        rm -f -- "$WRAP_LAST_DIR" > /dev/null
    }
}
