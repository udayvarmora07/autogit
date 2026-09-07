_autogit_complete() { COMPREPLY=( $(compgen -W 'version help completion doctor status plan logs config mcp init sync verify publish remote backup restore integrity repair retain export install uninstall' -- "${COMP_WORDS[COMP_CWORD]}") ); }
complete -F _autogit_complete autogit
