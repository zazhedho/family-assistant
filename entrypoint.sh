#!/bin/sh
set -e

if [ "${RUN_MIGRATION:-false}" = "true" ] || [ "${RUN_MIGRATION:-0}" = "1" ]; then
  case " $* " in
    *" -migrate "*) ;;
    *) set -- -migrate "$@" ;;
  esac
fi

exec ./main "$@"
