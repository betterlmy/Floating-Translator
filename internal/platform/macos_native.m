//go:build darwin

#import <ApplicationServices/ApplicationServices.h>
#import <Carbon/Carbon.h>
#import <Cocoa/Cocoa.h>
#import <dispatch/dispatch.h>
#include <time.h>

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

extern void goMacEventCallback(int eventID);

enum {
    macEventSelectionTranslate = 1,
    macEventToggleSelection = 2,
    macEventToggleListening = 3,
    macEventOpenSettings = 5,
    macEventOpenLogs = 7,
    macEventQuit = 8,
};

static void run_on_main_sync(dispatch_block_t block) {
    if ([NSThread isMainThread]) {
        block();
        return;
    }
    dispatch_sync(dispatch_get_main_queue(), block);
}

int macosRequestAccessibilityPermission(void) {
    __block int trusted = 0;
    run_on_main_sync(^{
        if (AXIsProcessTrusted()) {
            trusted = 1;
            return;
        }

        const void *keys[] = {kAXTrustedCheckOptionPrompt};
        const void *values[] = {kCFBooleanTrue};
        CFDictionaryRef options = CFDictionaryCreate(
            kCFAllocatorDefault,
            keys,
            values,
            1,
            &kCFTypeDictionaryKeyCallBacks,
            &kCFTypeDictionaryValueCallBacks);
        trusted = AXIsProcessTrustedWithOptions(options) ? 1 : 0;
        CFRelease(options);
    });
    return trusted;
}

static char *copy_utf8_string(NSString *value) {
    if (value == nil) {
        return NULL;
    }
    const char *utf8 = [value UTF8String];
    if (utf8 == NULL) {
        return NULL;
    }
    size_t length = strlen(utf8);
    if (length > 8 * 1024 * 1024) {
        return NULL;
    }
    char *result = (char *)malloc(length + 1);
    if (result == NULL) {
        return NULL;
    }
    memcpy(result, utf8, length + 1);
    return result;
}

typedef struct {
    NSArray *types;
    NSArray *data;
} macosClipboardSnapshot;

static NSStatusItem *trayStatusItem = nil;
static NSMenu *trayMenu = nil;
static NSMenuItem *trayStatusMenuItem = nil;
static NSMenuItem *trayListeningMenuItem = nil;
static NSMenuItem *traySelectionShortcutMenuItem = nil;
static NSMenuItem *traySelectionMenuItem = nil;
static NSObject *trayController = nil;
static char *trayShortcut = NULL;
static int trayStatus = 2;
static int trayListening = 0;
static int traySelectionEnabled = 0;

static id globalKeyMonitor = nil;
static id localKeyMonitor = nil;
static uint32_t configuredHotkeyModifiers = 0;
static uint32_t configuredHotkeyKey = 0;
static int configuredHotkeyEnabled = 0;

@interface FloatingTranslatorTrayController : NSObject
@end

@implementation FloatingTranslatorTrayController

- (void)menuAction:(id)sender {
    NSMenuItem *item = (NSMenuItem *)sender;
    goMacEventCallback((int)item.tag);
}

- (void)statusItemAction:(id)sender {
    NSEvent *event = [NSApp currentEvent];
    if (event.type == NSEventTypeRightMouseUp) {
        [trayStatusItem popUpStatusItemMenu:trayMenu];
        return;
    }
    goMacEventCallback(macEventOpenSettings);
}

@end

static NSMenuItem *new_tray_menu_item(NSString *title, NSInteger tag) {
    NSMenuItem *item = [[NSMenuItem alloc] initWithTitle:title
                                                   action:@selector(menuAction:)
                                            keyEquivalent:@""];
    [item setTarget:trayController];
    [item setTag:tag];
    return item;
}

static void update_tray_menu(void) {
    if (trayMenu == nil) {
        return;
    }

    if (trayStatus == 0) {
        trayStatusMenuItem.title = @"状态：正在监听";
    } else if (trayStatus == 1) {
        trayStatusMenuItem.title = @"状态：已暂停";
    } else {
        trayStatusMenuItem.title = @"状态：配置错误";
    }

    trayListeningMenuItem.title = trayListening ? @"暂停监听" : @"恢复监听";
    trayListeningMenuItem.enabled = trayStatus != 2;

    NSString *shortcut = trayShortcut == NULL
        ? @"未配置"
        : [NSString stringWithUTF8String:trayShortcut];
    traySelectionShortcutMenuItem.title =
        [NSString stringWithFormat:@"划词翻译快捷键：%@", shortcut];
    traySelectionMenuItem.title = traySelectionEnabled
        ? @"关闭划词翻译"
        : @"开启划词翻译";
    traySelectionMenuItem.enabled = trayStatus != 2;
}

static void create_tray_menu(void) {
    trayMenu = [[NSMenu alloc] initWithTitle:@"悬浮翻译器"];

    trayStatusMenuItem = [[NSMenuItem alloc] initWithTitle:@"状态：已暂停"
                                                     action:nil
                                              keyEquivalent:@""];
    trayStatusMenuItem.enabled = NO;
    [trayMenu addItem:trayStatusMenuItem];
    [trayStatusMenuItem release];

    [trayMenu addItem:[NSMenuItem separatorItem]];

    trayListeningMenuItem = new_tray_menu_item(@"恢复监听", macEventToggleListening);
    [trayMenu addItem:trayListeningMenuItem];
    [trayListeningMenuItem release];

    [trayMenu addItem:[NSMenuItem separatorItem]];

    traySelectionShortcutMenuItem =
        [[NSMenuItem alloc] initWithTitle:@"划词翻译快捷键：未配置"
                                   action:nil
                            keyEquivalent:@""];
    traySelectionShortcutMenuItem.enabled = NO;
    [trayMenu addItem:traySelectionShortcutMenuItem];
    [traySelectionShortcutMenuItem release];

    traySelectionMenuItem =
        new_tray_menu_item(@"开启划词翻译", macEventToggleSelection);
    [trayMenu addItem:traySelectionMenuItem];
    [traySelectionMenuItem release];

    [trayMenu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *settingsItem = new_tray_menu_item(@"设置…", macEventOpenSettings);
    [trayMenu addItem:settingsItem];
    [settingsItem release];
    NSMenuItem *logsItem = new_tray_menu_item(@"打开日志目录", macEventOpenLogs);
    [trayMenu addItem:logsItem];
    [logsItem release];

    [trayMenu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *quitItem = new_tray_menu_item(@"退出", macEventQuit);
    [trayMenu addItem:quitItem];
    [quitItem release];

    update_tray_menu();
}

void macosStartTray(void) {
    run_on_main_sync(^{
        if (trayStatusItem != nil) {
            return;
        }
        trayController = [[FloatingTranslatorTrayController alloc] init];
        trayStatusItem = [[[NSStatusBar systemStatusBar]
            statusItemWithLength:NSVariableStatusItemLength] retain];
        trayStatusItem.button.title = @"翻";
        trayStatusItem.button.toolTip = @"悬浮翻译器";
        create_tray_menu();
        trayStatusItem.button.target = trayController;
        trayStatusItem.button.action = @selector(statusItemAction:);
        [trayStatusItem.button sendActionOn:NSEventMaskLeftMouseUp | NSEventMaskRightMouseUp];
    });
}

void macosSetTrayState(int status, int listening, int selectionEnabled,
                       const char *shortcut) {
    run_on_main_sync(^{
        trayStatus = status;
        trayListening = listening;
        traySelectionEnabled = selectionEnabled;
        if (trayShortcut != NULL) {
            free(trayShortcut);
            trayShortcut = NULL;
        }
        if (shortcut != NULL) {
            trayShortcut = strdup(shortcut);
        }
        update_tray_menu();
    });
}

static void remove_key_monitors(void) {
    if (globalKeyMonitor != nil) {
        [NSEvent removeMonitor:globalKeyMonitor];
        [globalKeyMonitor release];
        globalKeyMonitor = nil;
    }
    if (localKeyMonitor != nil) {
        [NSEvent removeMonitor:localKeyMonitor];
        [localKeyMonitor release];
        localKeyMonitor = nil;
    }
}

static int function_key_for_code(unsigned short keyCode) {
    static const unsigned short keyCodes[] = {
        122, 120, 99, 118, 96, 97, 98, 100, 101, 109, 103, 111,
        105, 107, 113, 106, 64, 79, 80, 90,
    };
    for (int index = 0; index < (int)(sizeof(keyCodes) / sizeof(keyCodes[0]));
         index++) {
        if (keyCodes[index] == keyCode) {
            return 0x70 + index;
        }
    }
    return 0;
}

static int event_matches_hotkey(NSEvent *event) {
    if (!configuredHotkeyEnabled || event == nil ||
        event.type != NSEventTypeKeyDown) {
        return 0;
    }

    NSEventModifierFlags relevantFlags =
        NSEventModifierFlagOption | NSEventModifierFlagControl |
        NSEventModifierFlagShift | NSEventModifierFlagCommand;
    NSEventModifierFlags eventFlags = event.modifierFlags & relevantFlags;
    NSEventModifierFlags expectedFlags = 0;
    if (configuredHotkeyModifiers & 0x0001) {
        expectedFlags |= NSEventModifierFlagOption;
    }
    if (configuredHotkeyModifiers & 0x0002) {
        expectedFlags |= NSEventModifierFlagControl;
    }
    if (configuredHotkeyModifiers & 0x0004) {
        expectedFlags |= NSEventModifierFlagShift;
    }
    if (configuredHotkeyModifiers & 0x0008) {
        expectedFlags |= NSEventModifierFlagCommand;
    }
    if (eventFlags != expectedFlags) {
        return 0;
    }

    int functionKey = function_key_for_code(event.keyCode);
    if (functionKey != 0) {
        return configuredHotkeyKey == (uint32_t)functionKey;
    }

    NSString *characters =
        [[event charactersIgnoringModifiers] uppercaseString];
    if (characters.length == 0) {
        return 0;
    }
    unichar character = [characters characterAtIndex:0];
    if ((character >= 'A' && character <= 'Z') ||
        (character >= '0' && character <= '9')) {
        return configuredHotkeyKey == (uint32_t)character;
    }
    return 0;
}

static void install_key_monitors(void) {
    if (globalKeyMonitor == nil) {
        globalKeyMonitor = [[NSEvent addGlobalMonitorForEventsMatchingMask:
            NSEventMaskKeyDown
            handler:^(NSEvent *event) {
                if (event_matches_hotkey(event)) {
                    goMacEventCallback(macEventSelectionTranslate);
                }
            }] retain];
    }
    if (localKeyMonitor == nil) {
        localKeyMonitor = [[NSEvent addLocalMonitorForEventsMatchingMask:
            NSEventMaskKeyDown
            handler:^NSEvent *(NSEvent *event) {
                if (event_matches_hotkey(event)) {
                    goMacEventCallback(macEventSelectionTranslate);
                    return nil;
                }
                return event;
            }] retain];
    }
}

void macosSetSelectionHotkey(uint32_t modifiers, uint32_t key, int enabled) {
    run_on_main_sync(^{
        configuredHotkeyModifiers = modifiers;
        configuredHotkeyKey = key;
        configuredHotkeyEnabled = enabled;
        if (enabled) {
            install_key_monitors();
        } else {
            remove_key_monitors();
        }
    });
}

void macosStopTray(void) {
    run_on_main_sync(^{
        remove_key_monitors();
        if (trayStatusItem != nil) {
            [[NSStatusBar systemStatusBar] removeStatusItem:trayStatusItem];
            [trayStatusItem release];
            trayStatusItem = nil;
        }
        [trayMenu release];
        trayMenu = nil;
        [trayController release];
        trayController = nil;
        trayStatusMenuItem = nil;
        trayListeningMenuItem = nil;
        traySelectionShortcutMenuItem = nil;
        traySelectionMenuItem = nil;
        if (trayShortcut != NULL) {
            free(trayShortcut);
            trayShortcut = NULL;
        }
    });
}

int64_t macosClipboardChangeCount(void) {
    __block int64_t changeCount = 0;
    run_on_main_sync(^{
        changeCount = (int64_t)[[NSPasteboard generalPasteboard] changeCount];
    });
    return changeCount;
}

char *macosReadClipboard(void) {
    __block char *result = NULL;
    run_on_main_sync(^{
        NSString *text = [[NSPasteboard generalPasteboard]
            stringForType:NSPasteboardTypeString];
        result = copy_utf8_string(text);
    });
    return result;
}

void macosFreeString(char *value) {
    free(value);
}

void *macosClipboardSnapshotCreate(void) {
    __block macosClipboardSnapshot *snapshot = NULL;
    run_on_main_sync(^{
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        NSArray *types = [pasteboard types];
        if (types == nil || types.count == 0) {
            return;
        }

		NSMutableArray *snapshotTypes = [[NSMutableArray alloc] initWithCapacity:types.count];
		NSMutableArray *snapshotData = [[NSMutableArray alloc] initWithCapacity:types.count];
		for (NSString *type in types) {
			if (![type isEqualToString:NSPasteboardTypeString] &&
				![type isEqualToString:@"public.utf16-plain-text"] &&
				![type isEqualToString:@"public.utf16-external-plain-text"]) {
				[snapshotTypes release];
				[snapshotData release];
				return;
			}
			NSData *value = [pasteboard dataForType:type];
			if (value == nil) {
				[snapshotTypes release];
				[snapshotData release];
				return;
			}
			[snapshotTypes addObject:type];
			[snapshotData addObject:[[value copy] autorelease]];
		}
		if (snapshotTypes.count == 0) {
			[snapshotTypes release];
			[snapshotData release];
			return;
		}

		snapshot = (macosClipboardSnapshot *)calloc(1, sizeof(*snapshot));
		if (snapshot == NULL) {
			[snapshotTypes release];
			[snapshotData release];
			return;
		}
		snapshot->types = [[NSArray alloc] initWithArray:snapshotTypes];
		snapshot->data = [[NSArray alloc] initWithArray:snapshotData];
		[snapshotTypes release];
		[snapshotData release];
		if (snapshot->types == nil || snapshot->data == nil) {
			[snapshot->types release];
            [snapshot->data release];
            free(snapshot);
            snapshot = NULL;
        }
    });
    return snapshot;
}

void macosClipboardSnapshotRelease(void *value) {
    macosClipboardSnapshot *snapshot = (macosClipboardSnapshot *)value;
    if (snapshot == NULL) {
        return;
    }
    [snapshot->types release];
    [snapshot->data release];
    free(snapshot);
}

int macosClipboardSnapshotRestore(void *value) {
    macosClipboardSnapshot *snapshot = (macosClipboardSnapshot *)value;
    if (snapshot == NULL) {
        return 0;
    }

    __block int restored = 0;
    run_on_main_sync(^{
        NSPasteboard *pasteboard = [NSPasteboard generalPasteboard];
        [pasteboard clearContents];
        restored = 1;
        for (NSUInteger index = 0; index < snapshot->types.count; index++) {
            if (![pasteboard setData:snapshot->data[index]
                            forType:snapshot->types[index]]) {
                restored = 0;
                break;
            }
        }
    });
    return restored;
}

static int macosModifierKeysPressed(void) {
    const CGKeyCode modifierKeyCodes[] = {
        54, 55, 56, 60, 58, 61, 59, 62,
    };
    for (NSUInteger index = 0;
         index < sizeof(modifierKeyCodes) / sizeof(modifierKeyCodes[0]);
         index++) {
        if (CGEventSourceKeyState(
                kCGEventSourceStateHIDSystemState,
                modifierKeyCodes[index])) {
            return 1;
        }
    }
    return 0;
}

int macosWaitForModifierKeysReleased(int timeoutMilliseconds) {
    if (timeoutMilliseconds < 0) {
        timeoutMilliseconds = 0;
    }
    int elapsedMilliseconds = 0;
    const struct timespec interval = {0, 10 * 1000 * 1000};
    while (macosModifierKeysPressed()) {
        if (elapsedMilliseconds >= timeoutMilliseconds) {
            return 0;
        }
        nanosleep(&interval, NULL);
        elapsedMilliseconds += 10;
    }
    return 1;
}

int macosSendCopyShortcut(void) {
    if (!AXIsProcessTrusted()) {
        return 0;
    }
    CGEventSourceRef source =
        CGEventSourceCreate(kCGEventSourceStateHIDSystemState);
    if (source == NULL) {
        return 0;
    }
    CGEventRef keyDown = CGEventCreateKeyboardEvent(source, (CGKeyCode)8, true);
    CGEventRef keyUp = CGEventCreateKeyboardEvent(source, (CGKeyCode)8, false);
    if (keyDown == NULL || keyUp == NULL) {
        if (keyDown != NULL) {
            CFRelease(keyDown);
        }
        if (keyUp != NULL) {
            CFRelease(keyUp);
        }
        CFRelease(source);
        return 0;
    }
    CGEventSetFlags(keyDown, kCGEventFlagMaskCommand);
    CGEventSetFlags(keyUp, kCGEventFlagMaskCommand);
    CGEventPost(kCGHIDEventTap, keyDown);
    CGEventPost(kCGHIDEventTap, keyUp);
    CFRelease(keyDown);
    CFRelease(keyUp);
    CFRelease(source);
    return 1;
}

int macosAccessibilityTrusted(void) {
    return AXIsProcessTrusted() ? 1 : 0;
}

void macosOpenAccessibilitySettings(void) {
    run_on_main_sync(^{
        NSURL *url = [NSURL URLWithString:
            @"x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"];
        [[NSWorkspace sharedWorkspace] openURL:url];
    });
}

char *macosReadSelectedText(void) {
    if (!AXIsProcessTrusted()) {
        return NULL;
    }

    __block char *result = NULL;
    AXUIElementRef systemWide = AXUIElementCreateSystemWide();
    if (systemWide == NULL) {
        return NULL;
    }
    AXUIElementRef focused = NULL;
    AXError focusedError = AXUIElementCopyAttributeValue(
        systemWide, kAXFocusedUIElementAttribute, (CFTypeRef *)&focused);
    if (focusedError != kAXErrorSuccess || focused == NULL) {
        CFRelease(systemWide);
        return NULL;
    }
    CFTypeRef selected = NULL;
    AXError selectedError = AXUIElementCopyAttributeValue(
        focused, kAXSelectedTextAttribute, &selected);
    if (selectedError == kAXErrorSuccess && selected != NULL &&
        CFGetTypeID(selected) == CFStringGetTypeID()) {
        NSString *text = (NSString *)selected;
        result = copy_utf8_string(text);
    }
    if (selected != NULL) {
        CFRelease(selected);
    }
    CFRelease(focused);
    CFRelease(systemWide);
    return result;
}

int macosCursorPosition(double *x, double *y) {
    if (x == NULL || y == NULL) {
        return 0;
    }
    CGEventRef event = CGEventCreate(NULL);
    if (event == NULL) {
        return 0;
    }
    CGPoint location = CGEventGetLocation(event);
    CFRelease(event);
    *x = location.x;
    *y = location.y;
    return 1;
}

char *macosAvailableFontFamilies(void) {
    __block char *result = NULL;
    run_on_main_sync(^{
        NSArray<NSString *> *families =
            [[NSFontManager sharedFontManager] availableFontFamilies];
        NSMutableString *value = [[NSMutableString alloc] init];
        for (NSString *family in families) {
            if (family.length == 0) {
                continue;
            }
            [value appendString:family];
            [value appendString:@"\n"];
        }
        result = copy_utf8_string(value);
        [value release];
    });
    return result;
}
