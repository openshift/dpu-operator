#!/usr/bin/env bash

set -e

validate_venv() {
    local venv_path=$1
    local requirements_file=$2
    
    # Check if essential binaries exist and are executable
    if [[ ! -x "$venv_path/bin/python" ]] || [[ ! -x "$venv_path/bin/pip" ]]; then
        echo "Missing or non-executable python/pip binaries in venv"
        return 1
    fi
    
    # Test if python and pip are functional
    if ! "$venv_path/bin/python" --version >/dev/null 2>&1; then
        echo "Python binary in venv is not functional"
        return 1
    fi
    
    if ! "$venv_path/bin/pip" --version >/dev/null 2>&1; then
        echo "Pip binary in venv is not functional"
        return 1
    fi
    # If requirements file exists, check if all packages are installed
    if [[ -f "$requirements_file" ]]; then
        echo "Validating installed packages against $requirements_file..."
        
        # Create a temporary file with normalized requirements (no comments, no -e flags)
        local temp_req=$(mktemp)
        trap "rm -f '$temp_req'" RETURN
        
        grep -v '^#' "$requirements_file" | grep -v '^-e' | sed 's/[[:space:]]*#.*//' | grep -v '^[[:space:]]*$' > "$temp_req"
        
        # Check if pip check passes (validates package compatibility)
        if ! "$venv_path/bin/pip" check >/dev/null 2>&1; then
            echo "Pip check failed - package dependencies are broken"
            return 1
        fi
        
        # Verify each package in requirements is installed
        while IFS= read -r requirement; do
            if [[ -n "$requirement" ]]; then
                local pkg_name
                # Extract package name from lines in "requirements.txt"
                #
                # For URLs, we take the "filename" minus the version suffix.
                # We also strip version checks and extras ([]).
                pkg_name="$(printf '%s' "$requirement" \
                    | sed -e 's#^\(https://\|git+https://\).*/\([^-/@]\+\)[@-][^/]\+$#\2#' \
                          -e 's/[<>=!].*//' \
                          -e 's/\[.*\]//' \
                )"
                if ! "$venv_path/bin/pip" show "$pkg_name" >/dev/null 2>&1; then
                    echo "Required package '$pkg_name' is not installed"
                    return 1
                fi
            fi
        done < "$temp_req"
    fi
    
    echo "Venv validation passed"
    return 0
}

setup_venv() {
    local submodule_dir=$1
    local venv_path=$2
    local setup_script=$3
    local requirements_file=$4  # Optional requirements file for validation
    local sha_file=$venv_path/.submodule_sha

    cd $submodule_dir

    current_sha=$(git rev-parse HEAD)
    
    # Check SHA and venv existence first (fast checks)
    if [[ -d $venv_path && -f $sha_file && $(cat $sha_file) == "$current_sha" ]]; then
        echo "SHA matches for $submodule_dir, validating venv functionality..."
        
        # Validate venv functionality
        if validate_venv "$venv_path" "$requirements_file"; then
            echo "Virtual environment for $submodule_dir is up to date and functional. Skipping setup."
            cd -
            return 0
        else
            echo "Virtual environment for $submodule_dir exists but is not functional. Rebuilding..."
        fi
    else
        echo "Setting up virtual environment for $submodule_dir (SHA changed or venv missing)..."
    fi
    
    # Setup/rebuild venv
    rm -rf $venv_path
    python3.11 -m venv $venv_path
    source $venv_path/bin/activate
    eval $setup_script
    echo $current_sha > $sha_file
    deactivate
    
    echo "Setup completed for $submodule_dir"
    cd -
}

setup_all_venvs() {
    setup_venv cluster-deployment-automation /tmp/cda-venv "sh ./dependencies.sh" "requirements.txt"
    setup_venv kubernetes-traffic-flow-tests /tmp/tft-venv "pip install -U pip setuptools -r requirements.txt" "requirements.txt"
}

exec 9> "/tmp/dpu-operator-prepare-venv.lock"

while ! flock -n 9 ; do
  echo "Waiting for lock..."
  sleep 1
done

setup_all_venvs
