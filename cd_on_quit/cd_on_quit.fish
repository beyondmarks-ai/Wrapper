function wrap
    set os $(uname -s)

    if test "$os" = "Linux"
        set wrap_last_dir "$HOME/.local/state/wrapper/lastdir"
    end

    if test "$os" = "Darwin"
        set wrap_last_dir "$HOME/Library/Application Support/wrapper/lastdir"
    end

    command wrap $argv

    if test -f "$wrap_last_dir"
        source "$wrap_last_dir"
        rm -f -- "$wrap_last_dir" >> /dev/null
    end
end
