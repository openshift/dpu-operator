#! /bin/sh
# SPDX-License-Identifier: BSD-3-Clause
# Copyright 2015 6WIND S.A.

# Load config options:
# - PCIEP_CHECKPATCH_PATH
# - PCIEP_CHECKPATCH_CODESPELL
# - PCIEP_CHECKPATCH_LINE_LENGTH
# - PCIEP_CHECKPATCH_OPTIONS

VALIDATE_NEW_API=$(dirname $(readlink -f $0))/check-symbol-change.sh

# Enable codespell by default. This can be overwritten from a config file.
# Codespell can also be enabled by setting PCIEP_CHECKPATCH_CODESPELL to a valid path
# to a dictionary.txt file if dictionary.txt is not in the default location.
codespell=${PCIEP_CHECKPATCH_CODESPELL:-enable}
length=${PCIEP_CHECKPATCH_LINE_LENGTH:-100}

# override default Linux options
options="--no-tree"
if [ "$codespell" = "enable" ] ; then
    options="$options --codespell"
elif [ -f "$codespell" ] ; then
    options="$options --codespell"
    options="$options --codespellfile $codespell"
fi
options="$options --max-line-length=$length"
options="$options --show-types"
options="$options --ignore=LINUX_VERSION_CODE,ENOSYS,\
FILE_PATH_CHANGES,MAINTAINERS_STYLE,SPDX_LICENSE_TAG,\
VOLATILE,PREFER_PACKED,PREFER_ALIGNED,PREFER_PRINTF,STRLCPY,\
PREFER_KERNEL_TYPES,PREFER_FALLTHROUGH,BIT_MACRO,CONST_STRUCT,\
SPLIT_STRING,LONG_LINE_STRING,C99_COMMENT_TOLERANCE,\
LINE_SPACING,PARENTHESIS_ALIGNMENT,NETWORKING_BLOCK_COMMENT_STYLE,\
NEW_TYPEDEFS,COMPARISON_TO_NULL"
options="$options $PCIEP_CHECKPATCH_OPTIONS"

print_usage () {
	cat <<- END_OF_HELP
	usage: $(basename $0) [-h] [-q] [-v] [-nX|-r range|patch1 [patch2] ...]

	Run Linux kernel checkpatch.pl with DPDK options.
	The environment variable PCIEP_CHECKPATCH_PATH must be set.

	The patches to check can be from stdin, files specified on the command line,
	latest git commits limited with -n option, or commits in the git range
	specified with -r option (default: "origin/main..").
	END_OF_HELP
}

number=0
range='origin/main..'
quiet=false
verbose=false
while getopts hn:qr:v ARG ; do
	case $ARG in
		n ) number=$OPTARG ;;
		q ) quiet=true ;;
		r ) range=$OPTARG ;;
		v ) verbose=true ;;
		h ) print_usage ; exit 0 ;;
		? ) print_usage ; exit 1 ;;
	esac
done
shift $(($OPTIND - 1))

if [ ! -f "$PCIEP_CHECKPATCH_PATH" ] || [ ! -x "$PCIEP_CHECKPATCH_PATH" ] ; then
	print_usage >&2
	echo
	echo 'Cannot execute PCIEP_CHECKPATCH_PATH' >&2
	exit 1
fi

print_headline() { # <title>
	printf '\n### %s\n\n' "$1"
	headline_printed=true
}

total=0
status=0

check () { # <patch> <commit> <title>
	local ret=0
	headline_printed=false

	total=$(($total + 1))
	! $verbose || print_headline "$3"
	if [ -n "$1" ] ; then
		tmpinput=$1
	else
		tmpinput=$(mktemp -t pciep.checkpatches.XXXXXX)
		trap "rm -f '$tmpinput'" INT

		if [ -n "$2" ] ; then
			git format-patch --find-renames \
			--no-stat --stdout -1 $commit > "$tmpinput"
		else
			cat > "$tmpinput"
		fi
	fi

	! $verbose || printf 'Running checkpatch.pl:\n'
	report=$($PCIEP_CHECKPATCH_PATH $options "$tmpinput" 2>/dev/null)
	if [ $? -ne 0 ] ; then
		$headline_printed || print_headline "$3"
		printf '%s\n' "$report" | sed -n '1,/^total:.*lines checked$/p'
		ret=1
	fi

	! $verbose || printf '\nChecking API additions/removals:\n'
	report=$($VALIDATE_NEW_API "$tmpinput")
	if [ $? -ne 0 ] ; then
		$headline_printed || print_headline "$3"
		printf '%s\n' "$report"
		ret=1
	fi

	if [ "$tmpinput" != "$1" ]; then
		rm -f "$tmpinput"
		trap - INT
	fi
	[ $ret -eq 0 ] && return 0

	status=$(($status + 1))
}

if [ -n "$1" ] ; then
	for patch in "$@" ; do
		# Subject can be on 2 lines
		subject=$(sed '/^Subject: */!d;s///;N;s,\n[[:space:]]\+, ,;s,\n.*,,;q' "$patch")
		check "$patch" '' "$subject"
	done
elif [ ! -t 0 ] ; then # stdin
	subject=$(while read header value ; do
		if [ "$header" = 'Subject:' ] ; then
			IFS= read next
			continuation=$(echo "$next" | sed -n 's,^[[:space:]]\+, ,p')
			echo $value$continuation
			break
		fi
	done)
	check '' '' "$subject"
else
	if [ $number -eq 0 ] ; then
		commits=$(git rev-list --reverse $range)
	else
		commits=$(git rev-list --reverse --max-count=$number HEAD)
	fi
	for commit in $commits ; do
		subject=$(git log --format='%s' -1 $commit)
		check '' $commit "$subject"
	done
fi
pass=$(($total - $status))
$quiet || printf '\n%d/%d valid patch' $pass $total
$quiet || [ $pass -le 1 ] || printf 'es'
$quiet || printf '\n'
exit $status
