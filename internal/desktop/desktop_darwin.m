#import <Cocoa/Cocoa.h>

extern void aq_action(int identifier);
extern void aq_workspace(char *path);

@interface AQDocumentView : NSView
@end
@implementation AQDocumentView
- (BOOL)isFlipped { return YES; }
@end

@interface AQDesktop : NSObject <NSApplicationDelegate, NSWindowDelegate>
@property(nonatomic,strong) id originalDelegate;
@property(nonatomic,strong) NSWindow *window;
@property(nonatomic,strong) NSScrollView *scroll;
@property(nonatomic,strong) AQDocumentView *document;
@property(nonatomic,strong) NSStatusItem *statusItem;
@property(nonatomic,strong) NSDictionary *view;
@property(nonatomic,assign) BOOL showDetails;
@property(nonatomic,assign) BOOL showAllTasks;
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
- (NSTextField *)label:(NSString *)text frame:(NSRect)frame size:(CGFloat)size bold:(BOOL)bold {
 NSTextField *label=[NSTextField wrappingLabelWithString:text ?: @""];
 label.frame=frame;label.font=bold ? [NSFont boldSystemFontOfSize:size] : [NSFont systemFontOfSize:size];
 label.selectable=YES;[self.document addSubview:label];return label;
}
- (NSButton *)button:(NSString *)title identifier:(NSInteger)identifier frame:(NSRect)frame {
 NSButton *button=[NSButton buttonWithTitle:title target:self action:@selector(handleButton:)];
 button.frame=frame;button.tag=identifier;[self.document addSubview:button];return button;
}
- (void)render {
 if(!self.window || !self.view)return;
 for(NSView *child in self.document.subviews){[child removeFromSuperview];}
 CGFloat width=self.scroll.contentSize.width;
 CGFloat right=width-16,y=16;
 NSArray *tasks=[self.view[@"tasks"] isKindOfClass:NSArray.class] ? self.view[@"tasks"] : @[];
 [self label:self.view[@"summary"] frame:NSMakeRect(16,y,right-240,44) size:17 bold:YES];
 [self button:@"Check now" identifier:500 frame:NSMakeRect(right-212,y,100,28)];
 NSPopUpButton *manage=[[NSPopUpButton alloc] initWithFrame:NSMakeRect(right-104,y,104,28) pullsDown:YES];
 [manage addItemWithTitle:@"Manage"];
 for(NSDictionary *item in self.view[@"buttons"]){
  [manage addItemWithTitle:item[@"title"]];manage.lastItem.tag=[item[@"id"] integerValue];
 }
 manage.target=self;manage.action=@selector(handleManage:);[self.document addSubview:manage];
 y+=56;
 if(tasks.count==0){
  [self label:@"No action needed for confirmed checks." frame:NSMakeRect(16,y,right-16,22) size:13 bold:NO];y+=36;
 }
 NSUInteger count=self.showAllTasks ? tasks.count : MIN(tasks.count,5);
 for(NSUInteger index=0;index<count;index++){
  NSDictionary *task=tasks[index];NSDictionary *action=task[@"action"];
  [self label:task[@"title"] frame:NSMakeRect(16,y,right-144,22) size:14 bold:YES];
  NSInteger identifier=[action[@"id"] integerValue];
  if(identifier==0)identifier=-3;
  NSButton *repair=[self button:action[@"title"] ?: @"Show details" identifier:identifier frame:NSMakeRect(right-124,y-2,124,28)];
  repair.enabled=![action[@"disabled"] boolValue];
  [self label:task[@"detail"] frame:NSMakeRect(16,y+26,right-16,34) size:12 bold:NO];
  NSString *scope=task[@"scope"];
  if(scope.length){
   NSTextField *label=[self label:scope frame:NSMakeRect(16,y+62,right-16,18) size:11 bold:NO];label.textColor=NSColor.secondaryLabelColor;
  }
  NSBox *line=[[NSBox alloc] initWithFrame:NSMakeRect(16,y+86,right-16,1)];line.boxType=NSBoxSeparator;[self.document addSubview:line];
  y+=92;
 }
 if(tasks.count>5){
  NSString *title=self.showAllTasks ? @"Show fewer" : [NSString stringWithFormat:@"Show all %lu problems",(unsigned long)tasks.count];
  [self button:title identifier:-2 frame:NSMakeRect(16,y,190,28)];y+=40;
 }
 [self label:@"Quota" frame:NSMakeRect(16,y,right-16,22) size:14 bold:YES];y+=26;
 NSString *quota=self.view[@"quota"] ?: @"Waiting for quota data";
 NSUInteger lines=[[quota componentsSeparatedByString:@"\n"] count];
 CGFloat quotaHeight=MAX(22,lines*22);
 [self label:quota frame:NSMakeRect(16,y,right-16,quotaHeight) size:12 bold:NO];y+=quotaHeight+16;
 [self button:self.showDetails ? @"Hide details" : @"Show details" identifier:-1 frame:NSMakeRect(16,y,120,28)];y+=40;
 if(self.showDetails){
  NSScrollView *detailScroll=[[NSScrollView alloc] initWithFrame:NSMakeRect(16,y,right-16,360)];
  detailScroll.hasVerticalScroller=YES;detailScroll.borderType=NSBezelBorder;
  NSTextView *text=[[NSTextView alloc] initWithFrame:NSMakeRect(0,0,right-16,360)];
  text.editable=NO;text.selectable=YES;text.font=[NSFont systemFontOfSize:12];
  text.textContainerInset=NSMakeSize(10,10);text.autoresizingMask=NSViewWidthSizable;
  text.textContainer.widthTracksTextView=YES;text.string=self.view[@"details"] ?: @"";
  detailScroll.documentView=text;[self.document addSubview:detailScroll];y+=376;
 }
 [self.document setFrameSize:NSMakeSize(width,MAX(y,self.scroll.contentSize.height))];
}
- (void)windowDidResize:(NSNotification *)notification {(void)notification;[self render];}
- (void)showWindow {
 if(!self.window){
  self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,760,740) styleMask:(NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskResizable|NSWindowStyleMaskMiniaturizable) backing:NSBackingStoreBuffered defer:NO];
  self.window.title=@"AIQuota";self.window.releasedWhenClosed=NO;
  self.window.minSize=NSMakeSize(560,420);self.window.delegate=self;
  NSView *content=self.window.contentView;
  self.scroll=[[NSScrollView alloc] initWithFrame:content.bounds];
  self.scroll.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;self.scroll.hasVerticalScroller=YES;
  self.document=[[AQDocumentView alloc] initWithFrame:content.bounds];
  self.scroll.documentView=self.document;[content addSubview:self.scroll];[self render];
 }
 // Open on the display the user is interacting with, not the laptop by default.
 NSPoint pointer=NSEvent.mouseLocation;
 for(NSScreen *screen in NSScreen.screens){
  if(NSPointInRect(pointer,screen.frame)){
   NSRect visible=screen.visibleFrame;NSSize size=self.window.frame.size;
   [self.window setFrameOrigin:NSMakePoint(NSMidX(visible)-size.width/2,NSMidY(visible)-size.height/2)];break;
  }
 }
 [self.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
}
- (void)handleManage:(NSPopUpButton *)menu {
 [self handleIdentifier:menu.selectedItem.tag];[menu selectItemAtIndex:0];
}
- (void)handleButton:(NSButton *)button {[self handleIdentifier:button.tag];}
- (void)handleIdentifier:(NSInteger)identifier {
 if(identifier==-1){self.showDetails=!self.showDetails;[self render];return;}
 if(identifier==-2){self.showAllTasks=!self.showAllTasks;[self render];return;}
 if(identifier==-3){self.showDetails=YES;[self render];[self.document scrollPoint:NSMakePoint(0,MAX(0,self.document.frame.size.height-376))];return;}
 if(identifier==501){
  NSOpenPanel *panel=[NSOpenPanel openPanel];panel.canChooseDirectories=YES;panel.canChooseFiles=NO;panel.allowsMultipleSelection=NO;panel.prompt=@"Add workspace";
  [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse result){if(result==NSModalResponseOK){aq_workspace((char *)panel.URL.path.UTF8String);}}];
 } else {aq_action((int)identifier);}
}
- (void)wake:(NSNotification *)notification {(void)notification;self.statusItem.visible=YES;aq_action(500);}
- (void)screenChanged:(NSNotification *)notification {(void)notification;self.statusItem.visible=YES;}
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
   desktop.statusItem.autosaveName=@"AIQuota";desktop.statusItem.visible=YES;
  }
  NSApp.delegate=desktop;
  [[NSWorkspace sharedWorkspace].notificationCenter addObserver:desktop selector:@selector(wake:) name:NSWorkspaceDidWakeNotification object:nil];
  [[NSWorkspace sharedWorkspace].notificationCenter addObserver:desktop selector:@selector(screenChanged:) name:NSWorkspaceActiveSpaceDidChangeNotification object:nil];
  [[NSNotificationCenter defaultCenter] addObserver:desktop selector:@selector(screenChanged:) name:NSApplicationDidChangeScreenParametersNotification object:nil];
 });
}
void aq_show(void) {dispatch_async(dispatch_get_main_queue(), ^{[desktop showWindow];aq_action(504);});}
void aq_update(const char *view) {
 NSData *data=[[NSString stringWithUTF8String:view] dataUsingEncoding:NSUTF8StringEncoding];
 NSDictionary *snapshot=[NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
 dispatch_async(dispatch_get_main_queue(), ^{desktop.view=snapshot;[desktop render];});
}
