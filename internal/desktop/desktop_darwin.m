#import <Cocoa/Cocoa.h>

extern void aq_action(int identifier);
extern void aq_workspace(char *path);

@interface AQDesktop : NSObject <NSApplicationDelegate>
@property(nonatomic,strong) id originalDelegate;
@property(nonatomic,strong) NSWindow *window;
@property(nonatomic,strong) NSTextView *text;
@property(nonatomic,strong) NSStackView *actions;
@property(nonatomic,strong) NSStatusItem *statusItem;
@end

static AQDesktop *desktop;

@implementation AQDesktop
- (BOOL)respondsToSelector:(SEL)selector {
 return [super respondsToSelector:selector] || [self.originalDelegate respondsToSelector:selector];
}
- (id)forwardingTargetForSelector:(SEL)selector { return self.originalDelegate; }
- (BOOL)applicationShouldHandleReopen:(NSApplication *)sender hasVisibleWindows:(BOOL)visible {
 (void)sender; (void)visible; [self showWindow]; aq_action(504); return YES;
}
- (void)showWindow {
 if (!self.window) {
  self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,760,600) styleMask:(NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskResizable|NSWindowStyleMaskMiniaturizable) backing:NSBackingStoreBuffered defer:NO];
  self.window.title=@"AIQuota readiness";
  self.window.releasedWhenClosed=NO;
  self.window.minSize=NSMakeSize(560,380);
  NSView *content=self.window.contentView;
  NSScrollView *scroll=[[NSScrollView alloc] initWithFrame:NSMakeRect(16,84,728,500)];
  scroll.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;
  scroll.hasVerticalScroller=YES;
  self.text=[[NSTextView alloc] initWithFrame:NSMakeRect(0,0,728,500)];
  self.text.editable=NO;
  self.text.selectable=YES;
  self.text.font=[NSFont systemFontOfSize:14];
  self.text.textContainerInset=NSMakeSize(12,12);
  self.text.autoresizingMask=NSViewWidthSizable;
  self.text.textContainer.widthTracksTextView=YES;
  scroll.documentView=self.text; [content addSubview:scroll];
  NSScrollView *actionScroll=[[NSScrollView alloc] initWithFrame:NSMakeRect(16,16,728,56)];
  actionScroll.autoresizingMask=NSViewWidthSizable;
  actionScroll.hasHorizontalScroller=YES;
  self.actions=[[NSStackView alloc] initWithFrame:NSMakeRect(0,0,728,40)];
  self.actions.orientation=NSUserInterfaceLayoutOrientationHorizontal;
  self.actions.spacing=8; self.actions.alignment=NSLayoutAttributeCenterY;
  actionScroll.documentView=self.actions; [content addSubview:actionScroll];
 }
 // Open on the display the user is interacting with, not the laptop by default.
 NSPoint pointer=NSEvent.mouseLocation;
 for (NSScreen *screen in NSScreen.screens) {
  if (NSPointInRect(pointer,screen.frame)) {
   NSRect visible=screen.visibleFrame;NSSize size=self.window.frame.size;
   [self.window setFrameOrigin:NSMakePoint(NSMidX(visible)-size.width/2,NSMidY(visible)-size.height/2)];break;
  }
 }
 [self.window makeKeyAndOrderFront:nil]; [NSApp activateIgnoringOtherApps:YES];
}
- (void)handleButton:(NSButton *)button {
 if (button.tag==501) {
  NSOpenPanel *panel=[NSOpenPanel openPanel];panel.canChooseDirectories=YES;panel.canChooseFiles=NO;panel.allowsMultipleSelection=NO;panel.prompt=@"Monitor workspace";
  [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result){if(result==NSModalResponseOK){aq_workspace((char *)panel.URL.path.UTF8String);}}];
 } else { aq_action((int)button.tag); }
}
- (void)wake:(NSNotification *)notification { (void)notification; self.statusItem.visible=YES; aq_action(500); }
- (void)screenChanged:(NSNotification *)notification { (void)notification; self.statusItem.visible=YES; }
@end

void aq_configure(void) {
 dispatch_async(dispatch_get_main_queue(), ^{
  if(desktop)return;
  desktop=[AQDesktop new];desktop.originalDelegate=NSApp.delegate;
  // fyne.io/systray v1.12.2 owns this ivar. Keep this adapter isolated and fail
  // safely if the pinned library changes; no private macOS APIs are used.
  @try {desktop.statusItem=[desktop.originalDelegate valueForKey:@"statusItem"];} @catch(NSException *exception){(void)exception;}
  if(desktop.statusItem){
   NSUserDefaults *defaults=[NSUserDefaults standardUserDefaults];
   NSString *key=@"NSStatusItem Preferred Position AIQuota";
   if(![defaults objectForKey:key]){[defaults setDouble:0 forKey:key];}
   desktop.statusItem.autosaveName=@"AIQuota";
   desktop.statusItem.visible=YES;
  }
  NSApp.delegate=desktop;
  [[NSWorkspace sharedWorkspace].notificationCenter addObserver:desktop selector:@selector(wake:) name:NSWorkspaceDidWakeNotification object:nil];
  [[NSWorkspace sharedWorkspace].notificationCenter addObserver:desktop selector:@selector(screenChanged:) name:NSWorkspaceActiveSpaceDidChangeNotification object:nil];
  [[NSNotificationCenter defaultCenter] addObserver:desktop selector:@selector(screenChanged:) name:NSApplicationDidChangeScreenParametersNotification object:nil];
 });
}
void aq_show(void) {dispatch_async(dispatch_get_main_queue(), ^{[desktop showWindow];aq_action(504);});}
void aq_update(const char *content,const char *buttons) {
 NSString *text=[NSString stringWithUTF8String:content];
 NSData *data=[[NSString stringWithUTF8String:buttons] dataUsingEncoding:NSUTF8StringEncoding];
 NSArray *items=[NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
 dispatch_async(dispatch_get_main_queue(), ^{
  if(!desktop.window)return;
  desktop.text.string=text;
  for(NSView *view in desktop.actions.arrangedSubviews){[desktop.actions removeArrangedSubview:view];[view removeFromSuperview];}
  CGFloat width=0;
  for(NSDictionary *item in items){
   NSButton *button=[NSButton buttonWithTitle:item[@"title"] target:desktop action:@selector(handleButton:)];
   button.tag=[item[@"id"] integerValue];[button sizeToFit];width+=button.frame.size.width+8;[desktop.actions addArrangedSubview:button];
  }
  [desktop.actions setFrameSize:NSMakeSize(MAX(width,728),40)];
 });
}
