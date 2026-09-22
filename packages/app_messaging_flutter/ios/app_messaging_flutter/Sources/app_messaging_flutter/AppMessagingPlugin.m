#import "include/AppMessagingPlugin.h"
#import <AppMetricaPush/AppMetricaPush.h>
#import <UserNotifications/UserNotifications.h>

// Separate from the Flutter lifecycle delegate to avoid a delegate cycle when
// FlutterAppDelegate forwards notification callbacks to registered plugins.
@interface AMNotificationProxy : NSObject<UNUserNotificationCenterDelegate>
@property(nonatomic, weak) id<UNUserNotificationCenterDelegate> next;
@property(nonatomic, copy) void (^onTap)(UNNotificationResponse *);
@end

@implementation AMNotificationProxy
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
  didReceiveNotificationResponse:(UNNotificationResponse *)response
  withCompletionHandler:(void (^)(void))completion {
    [[AMPAppMetricaPush userNotificationCenterHandler] userNotificationCenterDidReceiveNotificationResponse:response];
    if (![response.actionIdentifier isEqualToString:UNNotificationDismissActionIdentifier]) self.onTap(response);
    if ([self.next respondsToSelector:_cmd]) {
        [self.next userNotificationCenter:center didReceiveNotificationResponse:response withCompletionHandler:completion];
    } else completion();
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
  willPresentNotification:(UNNotification *)notification
  withCompletionHandler:(void (^)(UNNotificationPresentationOptions))completion {
    [[AMPAppMetricaPush userNotificationCenterHandler] userNotificationCenterWillPresentNotification:notification];
    if ([AMPAppMetricaPush isNotificationRelatedToSDK:notification.request.content.userInfo]) {
        completion(UNNotificationPresentationOptionBanner | UNNotificationPresentationOptionList |
                   UNNotificationPresentationOptionSound | UNNotificationPresentationOptionBadge);
    } else if ([self.next respondsToSelector:_cmd]) {
        [self.next userNotificationCenter:center willPresentNotification:notification withCompletionHandler:completion];
    } else completion(UNNotificationPresentationOptionNone);
}
- (void)userNotificationCenter:(UNUserNotificationCenter *)center
  openSettingsForNotification:(UNNotification *)notification {
    [[AMPAppMetricaPush userNotificationCenterHandler] userNotificationCenterOpenSettingsForNotification:notification];
    if ([self.next respondsToSelector:_cmd]) [self.next userNotificationCenter:center openSettingsForNotification:notification];
}
@end

@interface AppMessagingPlugin ()
@property(nonatomic, strong) FlutterMethodChannel *channel;
@property(nonatomic, strong) NSMutableArray<NSDictionary *> *pending;
@property(nonatomic, strong) NSMutableOrderedSet<NSString *> *seen;
@property(nonatomic, strong) AMNotificationProxy *proxy;
@property(nonatomic, strong) NSData *token;
@property(nonatomic, strong) UISceneConnectionOptions *launchScene;
@property(nonatomic, strong) NSDictionary *legacyLaunch;
@property(nonatomic, assign) BOOL active;
@property(nonatomic, assign) BOOL listening;
@end

@implementation AppMessagingPlugin
+ (void)registerWithRegistrar:(NSObject<FlutterPluginRegistrar> *)registrar {
    AppMessagingPlugin *instance = [[AppMessagingPlugin alloc] init];
    instance.pending = [NSMutableArray array];
    instance.seen = [NSMutableOrderedSet orderedSet];
    instance.channel = [FlutterMethodChannel methodChannelWithName:@"dev.appbase/app_messaging" binaryMessenger:registrar.messenger];
    [registrar addMethodCallDelegate:instance channel:instance.channel];
    [registrar addApplicationDelegate:instance];
    [registrar addSceneDelegate:instance];
    [registrar publish:instance];
}

- (void)handleMethodCall:(FlutterMethodCall *)call result:(FlutterResult)result {
    if ([call.method isEqualToString:@"activate"]) {
        self.active = YES;
        [self sendToken];
        if (self.proxy == nil) {
            self.proxy = [[AMNotificationProxy alloc] init];
            __weak AppMessagingPlugin *weakSelf = self;
            self.proxy.onTap = ^(UNNotificationResponse *response) { [weakSelf capture:response]; };
        }
        UNUserNotificationCenter *center = UNUserNotificationCenter.currentNotificationCenter;
        if (center.delegate != self.proxy) {
            self.proxy.next = center.delegate;
            center.delegate = self.proxy;
        }
        if (self.launchScene != nil) {
            [AMPAppMetricaPush handleSceneWillConnectToSessionWithOptions:self.launchScene];
            self.launchScene = nil;
        }
        if (self.legacyLaunch != nil) {
            [AMPAppMetricaPush handleApplicationDidFinishLaunchingWithOptions:self.legacyLaunch];
            self.legacyLaunch = nil;
        }
        [UIApplication.sharedApplication registerForRemoteNotifications];
        self.listening = YES;
        NSArray *pending = [self.pending copy];
        [self.pending removeAllObjects];
        result(pending);
    } else if ([call.method isEqualToString:@"detach"]) {
        self.listening = NO;
        result(nil);
    } else result(FlutterMethodNotImplemented);
}

- (void)sendToken {
    if (!self.active || self.token == nil) return;
    // The configure tool adds this build-setting-derived Info.plist value.
    NSString *environment = [NSBundle.mainBundle objectForInfoDictionaryKey:@"AppMessagingAPNSEnvironment"];
    AMPAppMetricaPushEnvironment value = [environment isEqualToString:@"development"]
        ? AMPAppMetricaPushEnvironmentDevelopment : AMPAppMetricaPushEnvironmentProduction;
    [AMPAppMetricaPush setDeviceTokenFromData:self.token pushEnvironment:value];
}

- (void)application:(UIApplication *)application didRegisterForRemoteNotificationsWithDeviceToken:(NSData *)deviceToken {
    self.token = deviceToken;
    [self sendToken];
}

- (BOOL)application:(UIApplication *)application didFinishLaunchingWithOptions:(NSDictionary *)options {
    // Scene-based apps capture the explicit response below. Legacy apps use
    // launchOptions only for a visible notification opening, never a silent wake.
    NSDictionary *userInfo = options[UIApplicationLaunchOptionsRemoteNotificationKey];
    if ([NSBundle.mainBundle objectForInfoDictionaryKey:@"UIApplicationSceneManifest"] == nil &&
        application.applicationState != UIApplicationStateBackground &&
        userInfo[@"aps"][@"alert"] != nil && [AMPAppMetricaPush isNotificationRelatedToSDK:userInfo]) {
        NSString *payload = [AMPAppMetricaPush userDataForNotification:userInfo];
        if (payload != nil) [self.pending addObject:@{@"id": NSUUID.UUID.UUIDString, @"payload": payload}];
        self.legacyLaunch = options;
    }
    return YES;
}

- (void)capture:(UNNotificationResponse *)response {
    NSDictionary *userInfo = response.notification.request.content.userInfo;
    if (![AMPAppMetricaPush isNotificationRelatedToSDK:userInfo]) return;
    if ([response.actionIdentifier isEqualToString:UNNotificationDismissActionIdentifier]) return;
    NSString *payload = [AMPAppMetricaPush userDataForNotification:userInfo];
    if (payload == nil) return;
    NSString *identifier = [NSString stringWithFormat:@"%@:%@", response.notification.request.identifier, response.actionIdentifier];
    if ([self.seen containsObject:identifier]) return;
    [self.seen addObject:identifier];
    if (self.seen.count > 128) [self.seen removeObjectAtIndex:0];
    NSDictionary *action = @{@"id": identifier, @"payload": payload};
    if (self.listening) [self.channel invokeMethod:@"action" arguments:action];
    else [self.pending addObject:action];
}

- (BOOL)scene:(UIScene *)scene willConnectToSession:(UISceneSession *)session options:(UISceneConnectionOptions *)options {
    if (options.notificationResponse != nil) {
        [self capture:options.notificationResponse];
        if (self.active) [AMPAppMetricaPush handleSceneWillConnectToSessionWithOptions:options];
        else self.launchScene = options;
    }
    return NO;
}

- (BOOL)application:(UIApplication *)application didReceiveRemoteNotification:(NSDictionary *)userInfo
  fetchCompletionHandler:(void (^)(UIBackgroundFetchResult))completion {
    // A silent/background delivery must never cause application navigation.
    if (self.active && [AMPAppMetricaPush isNotificationRelatedToSDK:userInfo]) {
        [AMPAppMetricaPush handleRemoteNotification:userInfo];
        completion(UIBackgroundFetchResultNewData);
        return YES;
    }
    return NO;
}

- (void)detachFromEngineForRegistrar:(NSObject<FlutterPluginRegistrar> *)registrar {
    self.listening = NO;
    UNUserNotificationCenter *center = UNUserNotificationCenter.currentNotificationCenter;
    if (center.delegate == self.proxy) center.delegate = self.proxy.next;
}
@end
