#!/usr/bin/env bash

set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND_ADDR="${NEKOCLAW_ADDR:-127.0.0.1:8085}"
BACKEND_URL="${NEKOCLAW_BACKEND_URL:-http://${BACKEND_ADDR}}"
FRONTEND_DIR="${PROJECT_ROOT}/web"
HEALTHCHECK_URL="${BACKEND_URL}/healthz"
BACKEND_CMD=(go run ./cmd/nekoclaw -mode api)
FRONTEND_CMD=(npm run dev)

CYAN='\033[36m'
GREEN='\033[32m'
BLUE='\033[34m'
YELLOW='\033[33m'
RED='\033[31m'
RESET='\033[0m'

prefix_output() {
    local name="$1"
    local color="$2"
    local padded_name
    padded_name="$(printf '%-8s' "$name")"

    while IFS= read -r line; do
        printf '%b%s%b | %s\n' "${color}" "${padded_name}" "${RESET}" "${line}"
    done
}

print_header() {
    echo ""
    printf '%b+------------------------------------------+%b\n' "${CYAN}" "${RESET}"
    printf '%b|          NekoClaw Dev Servers            |%b\n' "${CYAN}" "${RESET}"
    printf '%b+------------------------------------------+%b\n' "${CYAN}" "${RESET}"
    printf '%b|%b  %bbackend %b | %s%b\n' "${CYAN}" "${RESET}" "${GREEN}" "${RESET}" "${BACKEND_URL}" "     ${CYAN}|${RESET}"
    printf '%b|%b  %bfrontend%b | http://localhost:5173     %b|%b\n' "${CYAN}" "${RESET}" "${BLUE}" "${RESET}" "${CYAN}" "${RESET}"
    printf '%b+------------------------------------------+%b\n' "${CYAN}" "${RESET}"
    echo ""
}

require_command() {
    local cmd="$1"
    if ! command -v "${cmd}" >/dev/null 2>&1; then
        printf '%b[system] %b | Missing command: %s\n' "${RED}" "${RESET}" "${cmd}" >&2
        exit 1
    fi
}

wait_for_backend() {
    local max_attempts=60
    local attempt=1

    while [ "${attempt}" -le "${max_attempts}" ]; do
        if ! kill -0 "${BACKEND_PID}" 2>/dev/null; then
            printf '%b[system] %b | Backend exited before it became ready\n' "${RED}" "${RESET}" >&2
            return 1
        fi
        if curl --silent --show-error --fail "${HEALTHCHECK_URL}" >/dev/null 2>&1; then
            printf '%b[system] %b | Backend is ready\n' "${YELLOW}" "${RESET}"
            return 0
        fi
        if [ "${attempt}" -eq 1 ]; then
            printf '%b[system] %b | Waiting for backend: %s\n' "${YELLOW}" "${RESET}" "${HEALTHCHECK_URL}"
        fi
        sleep 1
        attempt=$((attempt + 1))
    done

    printf '%b[system] %b | Backend health check timed out: %s\n' "${RED}" "${RESET}" "${HEALTHCHECK_URL}" >&2
    return 1
}

monitor_processes() {
    local backend_alive
    local frontend_alive
    local status
    local backend_status
    local frontend_status

    while true; do
        backend_alive=1
        frontend_alive=1
        if ! kill -0 "${BACKEND_PID}" 2>/dev/null; then
            backend_alive=0
        fi
        if ! kill -0 "${FRONTEND_PID}" 2>/dev/null; then
            frontend_alive=0
        fi

        if [ "${backend_alive}" -eq 1 ] && [ "${frontend_alive}" -eq 1 ]; then
            sleep 1
            continue
        fi

        status=0
        if [ "${backend_alive}" -eq 0 ]; then
            backend_status=0
            wait "${BACKEND_PID}" || backend_status=$?
            printf '%b[system] %b | Backend exited with status %s\n' "${YELLOW}" "${RESET}" "${backend_status}"
            status="${backend_status}"
        fi
        if [ "${frontend_alive}" -eq 0 ]; then
            frontend_status=0
            wait "${FRONTEND_PID}" || frontend_status=$?
            printf '%b[system] %b | Frontend exited with status %s\n' "${YELLOW}" "${RESET}" "${frontend_status}"
            if [ "${status}" -eq 0 ]; then
                status="${frontend_status}"
            fi
        fi

        cleanup "${status}"
    done
}

cleanup() {
    local exit_code="${1:-0}"

    echo ""
    printf '%b[system] %b | Shutting down servers...\n' "${YELLOW}" "${RESET}"

    if [ -n "${FRONTEND_PID:-}" ] && kill -0 "${FRONTEND_PID}" 2>/dev/null; then
        kill "${FRONTEND_PID}" 2>/dev/null || true
    fi
    if [ -n "${BACKEND_PID:-}" ] && kill -0 "${BACKEND_PID}" 2>/dev/null; then
        kill "${BACKEND_PID}" 2>/dev/null || true
    fi

    wait 2>/dev/null || true

    printf '%b[system] %b | Goodbye!\n' "${YELLOW}" "${RESET}"
    exit "${exit_code}"
}

on_signal() {
    cleanup 0
}

on_error() {
    local exit_code="$?"
    cleanup "${exit_code}"
}

require_command go
require_command npm
require_command curl

if [ ! -d "${FRONTEND_DIR}" ]; then
    printf '%b[system] %b | Frontend directory not found: %s\n' "${RED}" "${RESET}" "${FRONTEND_DIR}" >&2
    exit 1
fi

trap on_signal INT TERM
trap on_error ERR

print_header
printf '%b[system] %b | Starting backend first...\n' "${YELLOW}" "${RESET}"
printf '%b[system] %b | Press Ctrl+C to stop both servers\n' "${YELLOW}" "${RESET}"
echo ""

(
    cd "${PROJECT_ROOT}"
    "${BACKEND_CMD[@]}" 2>&1 | prefix_output "backend" "${GREEN}"
) &
BACKEND_PID=$!

wait_for_backend

printf '%b[system] %b | Starting frontend...\n' "${YELLOW}" "${RESET}"

(
    cd "${FRONTEND_DIR}"
    "${FRONTEND_CMD[@]}" 2>&1 | prefix_output "frontend" "${BLUE}"
) &
FRONTEND_PID=$!

monitor_processes
