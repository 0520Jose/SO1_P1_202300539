savedcmd_sysinfo.mod := printf '%s\n'   sysinfo.o | awk '!x[$$0]++ { print("./"$$0) }' > sysinfo.mod
