//go:build darwin && cgo

#import <Foundation/Foundation.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
	char *trashedPath;
	char *errorMessage;
} WRAPTrashResult;

static char *wrap_strdup_file_system_path(NSString *path) {
	if (path == nil) {
		return NULL;
	}
	const char *fsPath = [path fileSystemRepresentation];
	if (fsPath == NULL) {
		return NULL;
	}
	return strdup(fsPath);
}

WRAPTrashResult wrap_trash_item(const char *path) {
	WRAPTrashResult result = {0};
	@autoreleasepool {
		if (path == NULL) {
			result.errorMessage = strdup("missing path");
			return result;
		}

		NSString *pathString = [[NSFileManager defaultManager]
			stringWithFileSystemRepresentation:path
			length:strlen(path)];
		if (pathString == nil) {
			result.errorMessage = strdup("failed to create macOS file path");
			return result;
		}

		NSURL *url = [NSURL fileURLWithPath:pathString];
		NSURL *trashedURL = nil;
		NSError *error = nil;
		BOOL ok = [[NSFileManager defaultManager] trashItemAtURL:url
			resultingItemURL:&trashedURL
			error:&error];
		if (!ok) {
			NSString *message = error == nil
				? @"failed to move item to Trash"
				: [NSString stringWithFormat:@"%@ (%ld): %@",
					[error domain],
					(long)[error code],
					[error localizedDescription]];
			result.errorMessage = strdup([message UTF8String]);
			return result;
		}

		result.trashedPath = wrap_strdup_file_system_path([trashedURL path]);
		return result;
	}
}

void wrap_free_string(char *value) {
	if (value != NULL) {
		free(value);
	}
}
