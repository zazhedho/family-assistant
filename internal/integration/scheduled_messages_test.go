package integration

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	domainidentity "family-assistant/internal/domain/identity"
	domainreminder "family-assistant/internal/domain/reminder"
	domainscheduledmessage "family-assistant/internal/domain/scheduledmessage"
	domainspace "family-assistant/internal/domain/space"
	domainuser "family-assistant/internal/domain/user"
	"family-assistant/internal/dto"
	interfacenotification "family-assistant/internal/interfaces/notification"
	interfacescheduledmessage "family-assistant/internal/interfaces/scheduledmessage"
	interfacespace "family-assistant/internal/interfaces/space"
	auditrepo "family-assistant/internal/repositories/audit"
	identityrepo "family-assistant/internal/repositories/identity"
	permissionrepo "family-assistant/internal/repositories/permission"
	reminderrepo "family-assistant/internal/repositories/reminder"
	rolesrepo "family-assistant/internal/repositories/role"
	scheduledmessagerepo "family-assistant/internal/repositories/scheduledmessage"
	spacerepo "family-assistant/internal/repositories/space"
	userrepo "family-assistant/internal/repositories/user"
	serviceaudit "family-assistant/internal/services/audit"
	"family-assistant/internal/services/authorization"
	serviceidentity "family-assistant/internal/services/identity"
	servicepermission "family-assistant/internal/services/permission"
	servicereminder "family-assistant/internal/services/reminder"
	servicescheduledmessage "family-assistant/internal/services/scheduledmessage"
	"family-assistant/pkg/config"
	"family-assistant/utils"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // register the PostgreSQL migration driver
	_ "github.com/golang-migrate/migrate/v4/source/file"       // register the file migration source
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const scheduledMessageTestDatabaseEnv = "SCHEDULED_MESSAGE_TEST_DATABASE_URL"

func TestScheduledMessagesRealPostgresIntegration(t *testing.T) {
	db, migration := openScheduledMessageTestDatabase(t)
	ctx := context.Background()
	fixture := seedScheduledMessageFixture(t, ctx, db)

	spaces := spacerepo.NewRepository(db)
	permissions := servicepermission.NewPermissionService(permissionrepo.NewPermissionRepo(db))
	identities := serviceidentity.NewResolver(identityrepo.NewRepository(db), spaces, permissions)
	audits := serviceaudit.NewAuditService(auditrepo.NewAuditRepo(db))
	daddy := scheduledMessageActor(t, ctx, identities, permissions, fixture, "Daddy", fixture.sharedSpaceID)
	scheduledMessageActor(t, ctx, identities, permissions, fixture, "Mommy", fixture.sharedSpaceID)
	scheduledMessageActor(t, ctx, identities, permissions, fixture, "Member", fixture.sharedSpaceID)
	scheduledMessageActor(t, ctx, identities, permissions, fixture, "Outsider", fixture.personalSpaceIDs["Outsider"])

	sender := &recordingScheduledMessageSender{}
	legacyLane := legacyReminderLaneContext{
		ctx: ctx, db: db, spaces: spaces, identities: identities, audits: audits,
		actor: daddy, fixture: fixture, sender: sender,
	}
	legacyReminderID := exerciseLegacyReminderLane(t, legacyLane, "before-migration-12")
	assertPopulatedLegacyFixture(t, db, fixture, legacyReminderID)

	if err := migration.Steps(1); err != nil {
		t.Fatalf("apply additive migration 12 to populated schema: %v", err)
	}
	assertPopulatedLegacyFixture(t, db, fixture, legacyReminderID)
	assertScheduledMessageMigrationObjects(t, db, true)
	exerciseLegacyReminderLane(t, legacyLane, "after-migration-12")

	if err := migration.Steps(-1); err != nil {
		t.Fatalf("roll back migration 12 in disposable schema: %v", err)
	}
	assertScheduledMessageMigrationObjects(t, db, false)
	assertPopulatedLegacyFixture(t, db, fixture, legacyReminderID)
	exerciseLegacyReminderLane(t, legacyLane, "after-migration-12-down")
	if err := migration.Steps(1); err != nil {
		t.Fatalf("reapply migration 12 in disposable schema: %v", err)
	}
	assertScheduledMessageMigrationObjects(t, db, true)
	assertPopulatedLegacyFixture(t, db, fixture, legacyReminderID)
	exerciseLegacyReminderLane(t, legacyLane, "after-migration-12-reup")
	assertScheduledMessageDeliveryRetryTimeConstraint(t, db, fixture)

	permissions = servicepermission.NewPermissionService(permissionrepo.NewPermissionRepo(db))
	daddy = scheduledMessageActor(t, ctx, identities, permissions, fixture, "Daddy", fixture.sharedSpaceID)
	mommy := scheduledMessageActor(t, ctx, identities, permissions, fixture, "Mommy", fixture.sharedSpaceID)
	member := scheduledMessageActor(t, ctx, identities, permissions, fixture, "Member", fixture.sharedSpaceID)
	outsider := scheduledMessageActor(t, ctx, identities, permissions, fixture, "Outsider", fixture.personalSpaceIDs["Outsider"])

	repository := scheduledmessagerepo.NewRepository(db)
	service := servicescheduledmessage.NewScheduledMessageService(
		repository, spaces, authorization.NewAuthorizer(), audits, nil, nil,
		servicescheduledmessage.ScheduledMessageServiceOptions{MaxTextBytes: 2000, MaxListRows: 100},
	)
	testContext := scheduledMessageTestContext{
		ctx: ctx, db: db, repository: repository, service: service,
		daddy: daddy, mommy: mommy, member: member, outsider: outsider,
		fixture: fixture, sender: sender,
	}

	assertSelectedSpaceAndTargets(t, ctx, service, daddy, mommy, fixture)
	assertPrivateListVisibility(t, testContext)
	channel := bindIntegrationGroup(t, ctx, service, daddy, fixture.sharedSpaceID)
	testContext.channelID = channel.ID
	assertGroupCreatorCannotCancel(t, testContext)
	assertOccurrenceUpdateReusesRows(t, ctx, db, repository, service, daddy, fixture)
	assertRecipientUnsubscribeIsTerminal(t, ctx, db, service, daddy, member, fixture)
	assertScheduledDeliveryAndCancellationSafety(t, testContext)
	assertConcurrentAndStaleClaims(t, testContext)
}

type scheduledMessageFixture struct {
	userIDs           map[string]string
	personalSpaceIDs  map[string]string
	personalMemberIDs map[string]string
	sharedSpaceID     string
	sharedMemberIDs   map[string]string
	whatsappIDs       map[string]string
}

type scheduledMessageTestContext struct {
	ctx        context.Context
	db         *gorm.DB
	repository *scheduledmessagerepo.Repository
	service    interfacescheduledmessage.Service
	daddy      domainidentity.ActorContext
	mommy      domainidentity.ActorContext
	member     domainidentity.ActorContext
	outsider   domainidentity.ActorContext
	fixture    scheduledMessageFixture
	channelID  string
	sender     *recordingScheduledMessageSender
}

type legacyReminderLaneContext struct {
	ctx        context.Context
	db         *gorm.DB
	spaces     interfacespace.RepoSpaceInterface
	identities *serviceidentity.ResolverService
	audits     *serviceaudit.AuditService
	actor      domainidentity.ActorContext
	fixture    scheduledMessageFixture
	sender     *recordingScheduledMessageSender
}

type scheduledMessageSpaceInput struct {
	name      string
	spaceType string
	category  string
	userID    string
	roleID    string
}

type reusedOccurrenceExpectation struct {
	createdAt          time.Time
	content            string
	scheduledAt        time.Time
	status             domainscheduledmessage.DeliveryStatus
	generationAttempts int
	deliveryAttempts   int
}

func openScheduledMessageTestDatabase(t *testing.T) (*gorm.DB, *migrate.Migrate) {
	t.Helper()
	rawDSN, isSet := os.LookupEnv(scheduledMessageTestDatabaseEnv)
	if !isSet {
		t.Skip(scheduledMessageTestDatabaseEnv + " is not set")
	}
	dsn := strings.TrimSpace(rawDSN)
	if dsn == "" {
		t.Fatal(scheduledMessageTestDatabaseEnv + " must not be empty when set")
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal("invalid scheduled-message integration database URL")
	}
	address := net.ParseIP(parsed.Hostname())
	if address == nil || !address.IsLoopback() {
		t.Fatal(scheduledMessageTestDatabaseEnv + " must use a loopback IP host")
	}
	if strings.Trim(parsed.Path, "/") != "scheduled_message_integration_test" {
		t.Fatal(scheduledMessageTestDatabaseEnv + " must name the dedicated scheduled_message_integration_test database")
	}

	adminDB, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open dedicated scheduled-message test database: %v", err)
	}
	var databaseName string
	if err := adminDB.Raw("SELECT current_database()").Scan(&databaseName).Error; err != nil {
		t.Fatalf("verify scheduled-message database: %v", err)
	}
	if databaseName != "scheduled_message_integration_test" {
		t.Fatal("connected database does not match the dedicated scheduled-message test database")
	}

	schema := "scheduled_message_test_" + strings.ReplaceAll(utils.CreateUUID(), "-", "")
	if err := adminDB.Exec("CREATE SCHEMA \"" + schema + "\"").Error; err != nil {
		t.Fatalf("create isolated integration schema: %v", err)
	}

	scopedURL := *parsed
	query := scopedURL.Query()
	query.Set("search_path", schema)
	scopedURL.RawQuery = query.Encode()
	scopedDSN := scopedURL.String()
	var migration *migrate.Migrate
	var db *gorm.DB
	t.Cleanup(func() {
		if migration != nil {
			_, _ = migration.Close()
		}
		if db != nil {
			if sqlDB, dbErr := db.DB(); dbErr == nil {
				_ = sqlDB.Close()
			}
		}
		if err := adminDB.Exec("DROP SCHEMA IF EXISTS \"" + schema + "\" CASCADE").Error; err != nil {
			t.Errorf("drop owned scheduled-message test schema: %v", err)
		}
		if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	migration, err = migrate.New("file://../../migrations", scopedDSN)
	if err != nil {
		t.Fatalf("open migrations for isolated schema: %v", err)
	}
	if err := migration.Steps(11); err != nil {
		t.Fatalf("apply legacy migrations 1 through 11 in isolated schema: %v", err)
	}
	db, err = gorm.Open(gormpostgres.Open(scopedDSN), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open GORM on isolated migration schema: %v", err)
	}
	var schemaName string
	if err := db.Raw("SELECT current_schema()").Scan(&schemaName).Error; err != nil {
		t.Fatalf("verify isolated GORM search path: %v", err)
	}
	if schemaName != schema {
		t.Fatalf("GORM current_schema() = %q, want owned isolated schema", schemaName)
	}
	return db, migration
}

func seedScheduledMessageFixture(t *testing.T, ctx context.Context, db *gorm.DB) scheduledMessageFixture {
	t.Helper()
	roles := rolesrepo.NewRoleRepo(db)
	roleIDs := make(map[string]string, 3)
	for _, name := range []string{"space_owner", "space_admin", "space_member"} {
		role, err := roles.GetByName(ctx, name)
		if err != nil || role.Id == "" {
			t.Fatalf("load seeded role %q: role=%+v err=%v", name, role, err)
		}
		roleIDs[name] = role.Id
	}

	users := map[string]string{}
	userStore := userrepo.NewUserRepo(db)
	for _, name := range []string{"Daddy", "Mommy", "Member", "Outsider"} {
		user := domainuser.Users{Id: utils.CreateUUID(), Name: name, Email: strings.ToLower(name) + "@example.test", Role: "viewer", LoginProvider: "hermes"}
		if err := userStore.Store(ctx, user); err != nil {
			t.Fatalf("create %s fixture user: %v", name, err)
		}
		users[name] = user.Id
	}

	spaces := spacerepo.NewRepository(db)
	fixture := scheduledMessageFixture{
		userIDs: users, personalSpaceIDs: make(map[string]string), personalMemberIDs: make(map[string]string),
		sharedMemberIDs: make(map[string]string), whatsappIDs: map[string]string{
			"Daddy": "12025550101@s.whatsapp.net", "Mommy": "12025550102@s.whatsapp.net",
			"Member": "12025550103@s.whatsapp.net", "Outsider": "12025550104@s.whatsapp.net",
		},
	}
	for _, name := range []string{"Daddy", "Mommy", "Member", "Outsider"} {
		space, member := createScheduledMessageSpace(ctx, t, spaces, scheduledMessageSpaceInput{
			name: name + " personal", spaceType: domainspace.TypePersonal, category: domainspace.CategoryPersonal,
			userID: users[name], roleID: roleIDs["space_owner"],
		})
		fixture.personalSpaceIDs[name] = space.ID
		fixture.personalMemberIDs[name] = member.ID
	}
	shared, daddyMember := createScheduledMessageSpace(ctx, t, spaces, scheduledMessageSpaceInput{
		name: "Family", spaceType: domainspace.TypeShared, category: domainspace.CategoryFamily,
		userID: users["Daddy"], roleID: roleIDs["space_owner"],
	})
	fixture.sharedSpaceID = shared.ID
	fixture.sharedMemberIDs["Daddy"] = daddyMember.ID
	for _, member := range []struct {
		name   string
		roleID string
	}{
		{name: "Mommy", roleID: roleIDs["space_admin"]},
		{name: "Member", roleID: roleIDs["space_member"]},
	} {
		row := domainspace.Member{ID: utils.CreateUUID(), SpaceID: shared.ID, UserID: users[member.name], RoleID: member.roleID, Status: domainspace.StatusActive, CreatedAt: time.Now().UTC()}
		if err := db.WithContext(ctx).Create(&row).Error; err != nil {
			t.Fatalf("add %s to shared Space: %v", member.name, err)
		}
		fixture.sharedMemberIDs[member.name] = row.ID
	}

	for _, name := range []string{"Daddy", "Mommy", "Member", "Outsider"} {
		insertVerifiedHermesIdentity(t, ctx, db, users[name], fixture.whatsappIDs[name])
	}
	insertVerifiedHermesIdentity(t, ctx, db, users["Mommy"], "mommy-profile-not-a-whatsapp-id")
	return fixture
}

func createScheduledMessageSpace(ctx context.Context, t *testing.T, spaces interfacespace.RepoSpaceInterface, input scheduledMessageSpaceInput) (domainspace.Space, domainspace.Member) {
	t.Helper()
	now := time.Now().UTC()
	updatedAt := now
	space := domainspace.Space{
		ID: utils.CreateUUID(), Name: input.name, Type: input.spaceType, Category: input.category, Status: domainspace.StatusActive,
		CreatedByUserID: input.userID, CreatedAt: now, UpdatedAt: &updatedAt,
	}
	member := domainspace.Member{
		ID: utils.CreateUUID(), SpaceID: space.ID, UserID: input.userID, RoleID: input.roleID, Status: domainspace.StatusActive, CreatedAt: now,
	}
	if err := spaces.CreateWithOwner(ctx, &space, &member); err != nil {
		t.Fatalf("create %s fixture: %v", input.name, err)
	}
	return space, member
}

func insertVerifiedHermesIdentity(t *testing.T, ctx context.Context, db *gorm.DB, userID, externalID string) {
	t.Helper()
	now := time.Now().UTC()
	identity := domainidentity.ExternalIdentity{
		ID: utils.CreateUUID(), UserID: userID, Provider: domainidentity.ProviderHermes, ExternalID: externalID,
		Status: domainidentity.StatusActive, VerifiedAt: now, Metadata: map[string]any{"fixture": true}, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Create(&identity).Error; err != nil {
		t.Fatalf("create verified Hermes identity for %s: %v", userID, err)
	}
}

func scheduledMessageActor(t *testing.T, ctx context.Context, identities *serviceidentity.ResolverService, permissions *servicepermission.PermissionService, fixture scheduledMessageFixture, name, spaceID string) domainidentity.ActorContext {
	t.Helper()
	actor, err := identities.ResolveExternal(ctx, domainidentity.ProviderHermes, fixture.whatsappIDs[name], "whatsapp")
	if err != nil {
		t.Fatalf("resolve %s verified WhatsApp identity: %v", name, err)
	}
	selected, err := serviceidentity.SelectSpace(ctx, actor, spaceID, permissions)
	if err != nil {
		t.Fatalf("select %s active Space %s with current role permissions: %v", name, spaceID, err)
	}
	return selected
}

func exerciseLegacyReminderLane(t *testing.T, lane legacyReminderLaneContext, label string) string {
	t.Helper()
	ctx, db, spaces, identities, audits, actor, fixture, sender := lane.ctx, lane.db, lane.spaces, lane.identities, lane.audits, lane.actor, lane.fixture, lane.sender
	reminders := servicereminder.NewReminderService(reminderrepo.NewRepository(db), spaces, authorization.NewAuthorizer(), audits, nil)
	now := time.Now().UTC()
	delivered, err := reminders.Create(ctx, actor, dto.ReminderCreateInput{
		Space: fixture.sharedSpaceID, Title: label + " delivery", Description: "existing reminder lane",
		ScheduledAt: now.Add(-time.Minute), DeliveryProvider: "whatsapp", DeliveryTarget: fixture.whatsappIDs["Daddy"],
	})
	if err != nil {
		t.Fatalf("create %s reminder: %v", label, err)
	}
	future, err := reminders.Create(ctx, actor, dto.ReminderCreateInput{
		Space: fixture.sharedSpaceID, Title: label + " CRUD", ScheduledAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create %s CRUD reminder: %v", label, err)
	}
	updatedTitle := label + " CRUD updated"
	if _, err := reminders.Update(ctx, actor, fixture.sharedSpaceID, future.ID, dto.ReminderUpdateInput{Title: &updatedTitle}); err != nil {
		t.Fatalf("update %s reminder: %v", label, err)
	}
	listed, err := reminders.List(ctx, actor, dto.ReminderListInput{Space: fixture.sharedSpaceID})
	if err != nil || !containsReminder(listed, delivered.ID) || !containsReminder(listed, future.ID) {
		t.Fatalf("list %s reminders: rows=%d err=%v", label, len(listed), err)
	}
	deleted, err := reminders.Delete(ctx, actor, fixture.sharedSpaceID, future.ID)
	if err != nil || deleted.Status != domainreminder.StatusCancelled {
		t.Fatalf("delete %s reminder: row=%+v err=%v", label, deleted, err)
	}

	before := sender.count()
	scheduler := servicereminder.NewReminderScheduler(
		reminderrepo.NewSchedulerRepository(db), identities.IdentityRepo, spaces,
		map[string]interfacenotification.NotificationSender{"whatsapp": sender},
		servicereminder.ReminderSchedulerOptions{Lease: time.Minute, BatchSize: 10, DatabaseFallbackInterval: time.Second},
	)
	if err := scheduler.RunOnce(ctx, now); err != nil {
		t.Fatalf("run %s legacy reminder scheduler: %v", label, err)
	}
	if got := sender.count(); got != before+1 {
		t.Fatalf("%s legacy reminder sender calls = %d, want %d", label, got, before+1)
	}
	var persisted domainreminder.Reminder
	if err := db.WithContext(ctx).Where("id = ?", delivered.ID).First(&persisted).Error; err != nil {
		t.Fatalf("read %s delivered reminder: %v", label, err)
	}
	if persisted.Status != domainreminder.StatusSent || persisted.NotifiedAt == nil {
		t.Fatalf("%s reminder state = %s notified=%v, want SENT", label, persisted.Status, persisted.NotifiedAt)
	}
	return delivered.ID
}

func containsReminder(rows []domainreminder.Reminder, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}

func assertPopulatedLegacyFixture(t *testing.T, db *gorm.DB, fixture scheduledMessageFixture, reminderID string) {
	t.Helper()
	var counts struct {
		Users      int64
		Spaces     int64
		Members    int64
		Identities int64
	}
	if err := db.Raw(`SELECT
(SELECT COUNT(*) FROM users WHERE id IN ?) AS users,
(SELECT COUNT(*) FROM spaces WHERE id IN ?) AS spaces,
(SELECT COUNT(*) FROM space_members WHERE id IN ?) AS members,
(SELECT COUNT(*) FROM external_identities WHERE user_id IN ?) AS identities`,
		[]string{fixture.userIDs["Daddy"], fixture.userIDs["Mommy"], fixture.userIDs["Member"], fixture.userIDs["Outsider"]},
		[]string{fixture.sharedSpaceID, fixture.personalSpaceIDs["Daddy"], fixture.personalSpaceIDs["Mommy"], fixture.personalSpaceIDs["Member"], fixture.personalSpaceIDs["Outsider"]},
		[]string{fixture.sharedMemberIDs["Daddy"], fixture.sharedMemberIDs["Mommy"], fixture.sharedMemberIDs["Member"], fixture.personalMemberIDs["Daddy"], fixture.personalMemberIDs["Mommy"], fixture.personalMemberIDs["Member"], fixture.personalMemberIDs["Outsider"]},
		[]string{fixture.userIDs["Daddy"], fixture.userIDs["Mommy"], fixture.userIDs["Member"], fixture.userIDs["Outsider"]}).Scan(&counts).Error; err != nil {
		t.Fatalf("count populated legacy fixture: %v", err)
	}
	if counts.Users != 4 || counts.Spaces != 5 || counts.Members != 7 || counts.Identities != 5 {
		t.Fatalf("legacy fixture counts = %+v, want users=4 spaces=5 members=7 identities=5", counts)
	}
	var reminder domainreminder.Reminder
	if err := db.Where("id = ?", reminderID).First(&reminder).Error; err != nil {
		t.Fatalf("read legacy reminder %s: %v", reminderID, err)
	}
	if reminder.Status != domainreminder.StatusSent || reminder.NotifiedAt == nil {
		t.Fatalf("legacy reminder = status %s notified=%v, want SENT", reminder.Status, reminder.NotifiedAt)
	}
}

func assertScheduledMessageMigrationObjects(t *testing.T, db *gorm.DB, wantPresent bool) {
	t.Helper()
	for _, table := range []string{"space_channels", "scheduled_messages", "scheduled_message_deliveries"} {
		var exists bool
		if err := db.Raw("SELECT to_regclass(current_schema() || '.' || ?) IS NOT NULL", table).Scan(&exists).Error; err != nil {
			t.Fatalf("check migration table %s: %v", table, err)
		}
		if exists != wantPresent {
			t.Fatalf("migration table %s exists=%t, want %t", table, exists, wantPresent)
		}
	}
	var permissionCount int64
	if err := db.Table("permissions").Where("name IN ?", []string{
		"list_scheduled_messages", "view_scheduled_messages", "create_scheduled_messages", "update_scheduled_messages",
		"cancel_scheduled_messages", "unsubscribe_scheduled_messages", "bind_space_channels",
	}).Count(&permissionCount).Error; err != nil {
		t.Fatalf("count scheduled-message permissions: %v", err)
	}
	wantCount := int64(0)
	if wantPresent {
		wantCount = 7
	}
	if permissionCount != wantCount {
		t.Fatalf("scheduled-message permission rows=%d, want %d", permissionCount, wantCount)
	}
}

func assertSelectedSpaceAndTargets(t *testing.T, ctx context.Context, service interfacescheduledmessage.Service, daddy, mommy domainidentity.ActorContext, fixture scheduledMessageFixture) {
	t.Helper()
	if len(mommy.Memberships) != 2 || mommy.SpaceID != fixture.sharedSpaceID {
		t.Fatalf("Mommy resolved memberships=%d selected_space=%s, want personal+shared and selected shared Space", len(mommy.Memberships), mommy.SpaceID)
	}
	targets, err := service.Targets(ctx, daddy, fixture.sharedSpaceID)
	if err != nil {
		t.Fatalf("list scheduled-message targets: %v", err)
	}
	dmCount, groupCount, mommyCount := 0, 0, 0
	for _, target := range targets {
		switch target.Kind {
		case domainscheduledmessage.TargetDM:
			dmCount++
			if target.MemberID != nil && *target.MemberID == fixture.sharedMemberIDs["Mommy"] {
				mommyCount++
			}
		case domainscheduledmessage.TargetGroup:
			groupCount++
		}
	}
	if dmCount != 3 || groupCount != 0 || mommyCount != 1 {
		t.Fatalf("pre-bind targets: DM=%d GROUP=%d Mommy=%d; want 3/0/1 despite mixed non-WhatsApp identity", dmCount, groupCount, mommyCount)
	}
}

func assertPrivateListVisibility(t *testing.T, testContext scheduledMessageTestContext) {
	t.Helper()
	ctx, service, fixture := testContext.ctx, testContext.service, testContext.fixture
	start := time.Now().UTC().Add(20 * time.Minute)
	dm := createScheduledMessage(t, ctx, service, testContext.daddy, dto.ScheduledMessageCreateInput{
		Space: fixture.sharedSpaceID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(fixture.sharedMemberIDs["Mommy"]),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "private family note", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: start,
	})
	memberRows, err := service.List(ctx, testContext.member, dto.ScheduledMessageListInput{Space: fixture.sharedSpaceID})
	if err != nil {
		t.Fatalf("member list schedules: %v", err)
	}
	if containsScheduledMessage(memberRows, dm.ID) {
		t.Fatal("ordinary member can read a DM schedule for two other members")
	}
	mommyRows, err := service.List(ctx, testContext.mommy, dto.ScheduledMessageListInput{Space: fixture.sharedSpaceID})
	if err != nil || !containsScheduledMessage(mommyRows, dm.ID) {
		t.Fatalf("admin list DM schedule: found=%t err=%v", containsScheduledMessage(mommyRows, dm.ID), err)
	}
	outsider := testContext.outsider
	outsider.SpaceID = fixture.sharedSpaceID
	if _, err := service.List(ctx, outsider, dto.ScheduledMessageListInput{Space: fixture.sharedSpaceID}); err == nil {
		t.Fatal("outsider without active shared membership listed shared schedules")
	}
	if _, err := service.Cancel(ctx, testContext.daddy, fixture.sharedSpaceID, dm.ID); err != nil {
		t.Fatalf("cancel private-visibility fixture schedule: %v", err)
	}
}

func bindIntegrationGroup(t *testing.T, ctx context.Context, service interfacescheduledmessage.Service, owner domainidentity.ActorContext, spaceID string) *domainscheduledmessage.SpaceChannel {
	t.Helper()
	channel, err := service.BindCurrentGroup(ctx, owner, dto.SpaceChannelBindInput{
		Space: spaceID, CurrentChannel: "whatsapp", CurrentChatID: "120363012345678901@g.us", CurrentChatType: "group",
	})
	if err != nil {
		t.Fatalf("bind current WhatsApp group: %v", err)
	}
	return channel
}

func assertGroupCreatorCannotCancel(t *testing.T, testContext scheduledMessageTestContext) {
	t.Helper()
	ctx, service, spaceID, channelID := testContext.ctx, testContext.service, testContext.fixture.sharedSpaceID, testContext.channelID
	schedule := createScheduledMessage(t, ctx, service, testContext.member, dto.ScheduledMessageCreateInput{
		Space: spaceID, Target: domainscheduledmessage.TargetGroup, SpaceChannelID: stringPointer(channelID),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "group cancellation policy", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: time.Now().UTC().Add(time.Hour),
	})
	content := "group schedule edited by its creator"
	updated, err := service.Update(ctx, testContext.member, spaceID, schedule.ID, dto.ScheduledMessageUpdateInput{Content: &content})
	if err != nil {
		t.Fatalf("ordinary group creator Update() of own schedule: %v", err)
	}
	if updated == nil || updated.Content != content || updated.Target != domainscheduledmessage.TargetGroup || updated.SpaceChannelID == nil || *updated.SpaceChannelID != channelID {
		t.Fatalf("ordinary group creator Update() changed ownership/destination: %#v", updated)
	}
	if _, err := service.Cancel(ctx, testContext.member, spaceID, schedule.ID); !errors.Is(err, authorization.ErrForbidden) {
		t.Fatalf("ordinary group creator Cancel() error = %v, want forbidden", err)
	}
	adminCancelled, err := service.Cancel(ctx, testContext.mommy, spaceID, schedule.ID)
	if err != nil {
		t.Fatalf("Space admin Cancel() of group schedule: %v", err)
	}
	if adminCancelled == nil {
		t.Fatal("Space admin Cancel() returned no schedule")
	}
	if adminCancelled.Status != domainscheduledmessage.ScheduleStatusCancelled {
		t.Fatalf("Space admin Cancel() status=%s, want CANCELLED", adminCancelled.Status)
	}
	ownerSchedule := createScheduledMessage(t, ctx, service, testContext.member, dto.ScheduledMessageCreateInput{
		Space: spaceID, Target: domainscheduledmessage.TargetGroup, SpaceChannelID: stringPointer(channelID),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "group owner cancellation policy", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: time.Now().UTC().Add(time.Hour),
	})
	cancelled, err := service.Cancel(ctx, testContext.daddy, spaceID, ownerSchedule.ID)
	if err != nil {
		t.Fatalf("Space owner Cancel() of group schedule: %v", err)
	}
	if cancelled == nil {
		t.Fatal("Space owner Cancel() returned no schedule")
	}
	if cancelled.Status != domainscheduledmessage.ScheduleStatusCancelled {
		t.Fatalf("Space owner Cancel() of group schedule: status=%s err=%v, want CANCELLED", cancelled.Status, err)
	}
}

func assertOccurrenceUpdateReusesRows(t *testing.T, ctx context.Context, db *gorm.DB, repository *scheduledmessagerepo.Repository, service interfacescheduledmessage.Service, creator domainidentity.ActorContext, fixture scheduledMessageFixture) {
	t.Helper()
	startAt := time.Now().UTC().Add(2 * time.Hour)
	schedule := createScheduledMessage(t, ctx, service, creator, dto.ScheduledMessageCreateInput{
		Space: fixture.sharedSpaceID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(fixture.sharedMemberIDs["Mommy"]),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "original occurrence content", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: startAt,
	})
	var original domainscheduledmessage.Delivery
	if err := db.Where("scheduled_message_id = ?", schedule.ID).First(&original).Error; err != nil {
		t.Fatalf("read initial scheduled-message occurrence: %v", err)
	}
	createdAt := original.CreatedAt
	leaseToken := "00000000-0000-4000-8000-000000000901"
	leaseUntil := startAt.Add(time.Hour)
	if err := db.Model(&domainscheduledmessage.Delivery{}).Where("id = ?", original.ID).Updates(map[string]any{
		"generation_attempts": 2,
		"delivery_attempts":   3,
		"lease_token":         leaseToken,
		"lease_until":         leaseUntil,
	}).Error; err != nil {
		t.Fatalf("seed occurrence attempt and lease state: %v", err)
	}

	content := "content-only edit at the same occurrence"
	want := reusedOccurrenceExpectation{
		createdAt: createdAt, content: content, scheduledAt: startAt,
		status: domainscheduledmessage.DeliveryStatusPending, generationAttempts: 2, deliveryAttempts: 3,
	}
	if _, err := service.Update(ctx, creator, fixture.sharedSpaceID, schedule.ID, dto.ScheduledMessageUpdateInput{Content: &content}); err != nil {
		t.Fatalf("content-only edit at the same occurrence: %v", err)
	}
	assertReusedOccurrenceState(t, db, schedule.ID, original.ID, want)
	if _, _, _, _, _, err := repository.LoadClaimed(ctx, original.ID, leaseToken, time.Now().UTC()); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("old lease after same-UUID reuse error=%v, want no matching claim", err)
	}
	if count := countScheduledMessageDeliveries(t, db, schedule.ID); count != 1 {
		t.Fatalf("deliveries after content-only edit = %d, want one reused row", count)
	}

	repeatedContent := "repeated edit at the same occurrence"
	want.content = repeatedContent
	if _, err := service.Update(ctx, creator, fixture.sharedSpaceID, schedule.ID, dto.ScheduledMessageUpdateInput{Content: &repeatedContent}); err != nil {
		t.Fatalf("repeated edit at the same occurrence: %v", err)
	}
	assertReusedOccurrenceState(t, db, schedule.ID, original.ID, want)

	secondAt := time.Now().UTC().Add(3 * time.Hour)
	if _, err := service.Update(ctx, creator, fixture.sharedSpaceID, schedule.ID, dto.ScheduledMessageUpdateInput{StartAt: &secondAt}); err != nil {
		t.Fatalf("move occurrence from A to B: %v", err)
	}
	if err := db.Where("scheduled_message_id = ? AND scheduled_at = ?", schedule.ID, startAt).First(&original).Error; err != nil {
		t.Fatalf("read superseded A occurrence: %v", err)
	}
	if original.Status != domainscheduledmessage.DeliveryStatusCancelled {
		t.Fatalf("A occurrence after A-to-B update status=%s, want CANCELLED", original.Status)
	}
	var second domainscheduledmessage.Delivery
	if err := db.Where("scheduled_message_id = ? AND scheduled_at = ?", schedule.ID, secondAt).First(&second).Error; err != nil {
		t.Fatalf("read replacement B occurrence: %v", err)
	}
	if second.Status != domainscheduledmessage.DeliveryStatusPending {
		t.Fatalf("B occurrence after A-to-B update status=%s, want PENDING", second.Status)
	}

	if _, err := service.Update(ctx, creator, fixture.sharedSpaceID, schedule.ID, dto.ScheduledMessageUpdateInput{StartAt: &startAt}); err != nil {
		t.Fatalf("move occurrence from B back to A: %v", err)
	}
	assertReusedOccurrenceState(t, db, schedule.ID, original.ID, want)
	if err := db.Where("id = ?", second.ID).First(&second).Error; err != nil {
		t.Fatalf("read superseded B occurrence: %v", err)
	}
	if second.Status != domainscheduledmessage.DeliveryStatusCancelled {
		t.Fatalf("B occurrence after B-to-A update status=%s, want CANCELLED", second.Status)
	}
	if count := countScheduledMessageDeliveries(t, db, schedule.ID); count != 2 {
		t.Fatalf("deliveries after A-to-B-to-A edits = %d, want two historical rows", count)
	}

	sentStartAt := time.Now().UTC().Add(4 * time.Hour)
	sentSchedule := createScheduledMessage(t, ctx, service, creator, dto.ScheduledMessageCreateInput{
		Space: fixture.sharedSpaceID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(fixture.sharedMemberIDs["Mommy"]),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "already-sent occurrence", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: sentStartAt,
	})
	var sent domainscheduledmessage.Delivery
	if err := db.Where("scheduled_message_id = ?", sentSchedule.ID).First(&sent).Error; err != nil {
		t.Fatalf("read occurrence for SENT immutability check: %v", err)
	}
	sentAt := time.Now().UTC()
	if err := db.Model(&domainscheduledmessage.Delivery{}).Where("id = ?", sent.ID).Updates(map[string]any{
		"status":  domainscheduledmessage.DeliveryStatusSent,
		"sent_at": sentAt,
	}).Error; err != nil {
		t.Fatalf("seed SENT occurrence: %v", err)
	}
	if _, err := service.Update(ctx, creator, fixture.sharedSpaceID, sentSchedule.ID, dto.ScheduledMessageUpdateInput{Content: &content}); !errors.Is(err, interfacescheduledmessage.ErrConflict) {
		t.Fatalf("update over SENT occurrence error=%v, want conflict", err)
	}
	if err := db.Where("id = ?", sent.ID).First(&sent).Error; err != nil {
		t.Fatalf("read SENT occurrence after rejected edit: %v", err)
	}
	if sent.Status != domainscheduledmessage.DeliveryStatusSent || sent.SentAt == nil || !sent.SentAt.Equal(sentAt) {
		t.Fatalf("SENT occurrence after rejected edit=%+v, want immutable SENT history", sent)
	}
}

func assertReusedOccurrenceState(t *testing.T, db *gorm.DB, scheduleID, deliveryID string, want reusedOccurrenceExpectation) {
	t.Helper()
	var occurrence domainscheduledmessage.Delivery
	if err := db.Where("id = ?", deliveryID).First(&occurrence).Error; err != nil {
		t.Fatalf("read reused occurrence %s: %v", deliveryID, err)
	}
	if occurrence.ScheduledMessageID != scheduleID || occurrence.Status != want.status || !occurrence.ScheduledAt.Equal(want.scheduledAt) || occurrence.GenerationAttempts != want.generationAttempts || occurrence.DeliveryAttempts != want.deliveryAttempts || !occurrence.CreatedAt.Equal(want.createdAt) {
		t.Fatalf("reused occurrence=%+v, want schedule=%s status=%s scheduled_at=%s attempts=%d/%d created_at=%s", occurrence, scheduleID, want.status, want.scheduledAt, want.generationAttempts, want.deliveryAttempts, want.createdAt)
	}
	if occurrence.LeaseToken != nil || occurrence.LeaseExpiresAt != nil {
		t.Fatalf("reused occurrence retains a lease: token=%v until=%v", occurrence.LeaseToken, occurrence.LeaseExpiresAt)
	}
	if occurrence.ContentSnapshot == nil || *occurrence.ContentSnapshot != want.content {
		t.Fatalf("reused occurrence content snapshot=%v, want %q", occurrence.ContentSnapshot, want.content)
	}
}

func countScheduledMessageDeliveries(t *testing.T, db *gorm.DB, scheduleID string) int64 {
	t.Helper()
	var count int64
	if err := db.Model(&domainscheduledmessage.Delivery{}).Where("scheduled_message_id = ?", scheduleID).Count(&count).Error; err != nil {
		t.Fatalf("count scheduled-message deliveries: %v", err)
	}
	return count
}

func assertRecipientUnsubscribeIsTerminal(t *testing.T, ctx context.Context, db *gorm.DB, service interfacescheduledmessage.Service, creator, recipient domainidentity.ActorContext, fixture scheduledMessageFixture) {
	t.Helper()
	schedule := createScheduledMessage(t, ctx, service, creator, dto.ScheduledMessageCreateInput{
		Space: fixture.sharedSpaceID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(recipient.MemberID),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "unsubscribe before delivery", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: time.Now().UTC().Add(30 * time.Minute),
	})
	unsubscribed, err := service.Unsubscribe(ctx, recipient, fixture.sharedSpaceID, schedule.ID)
	if err != nil || unsubscribed.Status != domainscheduledmessage.ScheduleStatusUnsubscribed {
		t.Fatalf("recipient unsubscribe result=%+v err=%v", unsubscribed, err)
	}
	if _, err := service.Update(ctx, creator, fixture.sharedSpaceID, schedule.ID, dto.ScheduledMessageUpdateInput{Content: stringPointer("resubscribe attempt")}); !errors.Is(err, interfacescheduledmessage.ErrConflict) {
		t.Fatalf("update unsubscribed schedule error=%v, want conflict", err)
	}
	var delivery domainscheduledmessage.Delivery
	if err := db.Where("scheduled_message_id = ?", schedule.ID).First(&delivery).Error; err != nil {
		t.Fatalf("read unsubscribed delivery: %v", err)
	}
	if delivery.Status != domainscheduledmessage.DeliveryStatusCancelled || delivery.LeaseToken != nil || delivery.LeaseExpiresAt != nil {
		t.Fatalf("unsubscribed delivery state=%+v, want terminal CANCELLED without lease", delivery)
	}
}

func assertScheduledDeliveryAndCancellationSafety(t *testing.T, testContext scheduledMessageTestContext) {
	t.Helper()
	ctx, db, repository, service := testContext.ctx, testContext.db, testContext.repository, testContext.service
	creator, recipient, fixture := testContext.daddy, testContext.member, testContext.fixture
	channelID, sender := testContext.channelID, testContext.sender
	due := time.Now().UTC().Add(5 * time.Minute)
	dm := createScheduledMessage(t, ctx, service, creator, dto.ScheduledMessageCreateInput{
		Space: fixture.sharedSpaceID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(recipient.MemberID),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "fixed DM delivered", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: due,
	})
	group := createScheduledMessage(t, ctx, service, creator, dto.ScheduledMessageCreateInput{
		Space: fixture.sharedSpaceID, Target: domainscheduledmessage.TargetGroup, SpaceChannelID: stringPointer(channelID),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "fixed group delivered", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: due,
	})
	before := sender.count()
	runAt := due.Add(time.Minute)
	scheduledConfig := testScheduledMessageConfig()
	scheduler, err := servicescheduledmessage.NewScheduledMessageScheduler(
		repository, nil, nil, sender, scheduledConfig, dto.ScheduledMessageSchedulerOptions{Now: func() time.Time { return runAt }},
	)
	if err != nil {
		t.Fatalf("construct scheduled-message scheduler: %v", err)
	}
	if err := scheduler.RunOnce(ctx, runAt); err != nil {
		t.Fatalf("run fixed-message DM/group deliveries: %v", err)
	}
	if got := sender.count(); got != before+2 {
		t.Fatalf("scheduled-message sender calls=%d, want %d; sends must stay at the fake boundary", got, before+2)
	}
	for _, id := range []string{dm.ID, group.ID} {
		var schedule domainscheduledmessage.Schedule
		if err := db.Where("id = ?", id).First(&schedule).Error; err != nil {
			t.Fatalf("read delivered schedule %s: %v", id, err)
		}
		if schedule.Status != domainscheduledmessage.ScheduleStatusSent {
			t.Fatalf("delivered schedule %s status=%s, want SENT", id, schedule.Status)
		}
	}
	if got := sender.targetsAfter(before); len(got) != 2 || !containsString(got, fixture.whatsappIDs["Member"]) || !containsString(got, "120363012345678901@g.us") {
		t.Fatalf("scheduled sender targets=%v, want Member WhatsApp and bound group only", got)
	}

	assertFinalGateMutation(t, testContext, "cancel")
	assertFinalGateMutation(t, testContext, "unsubscribe")
	assertFinalGateMutation(t, testContext, "update")
}

func assertFinalGateMutation(t *testing.T, testContext scheduledMessageTestContext, action string) {
	t.Helper()
	ctx, service := testContext.ctx, testContext.service
	fixture, recipient, creator := testContext.fixture, testContext.member, testContext.daddy
	due := time.Now().UTC().Add(20 * time.Minute)
	input := dto.ScheduledMessageCreateInput{
		Space: fixture.sharedSpaceID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(recipient.MemberID),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "final gate " + action, Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: due,
	}
	if action == "group-cancel" {
		input.Target, input.RecipientMemberID, input.SpaceChannelID = domainscheduledmessage.TargetGroup, nil, stringPointer(testContext.channelID)
	}
	schedule := createScheduledMessage(t, ctx, service, creator, input)
	claim := claimFinalGateDelivery(t, testContext, schedule.ID, due, action)
	applyFinalGateMutation(t, testContext, schedule.ID, due, action)
	dispatched := false
	err := testContext.repository.DispatchAndFinalize(ctx, claim.ID, *claim.LeaseToken, due.Add(2*time.Second), nil,
		func(context.Context, *domainscheduledmessage.Schedule, *domainscheduledmessage.Delivery, *domainscheduledmessage.SpaceChannel, *domainidentity.ExternalIdentity) (time.Time, error) {
			dispatched = true
			return due.Add(2 * time.Second), nil
		})
	if !errors.Is(err, interfacescheduledmessage.ErrClaimConflict) || dispatched {
		t.Fatalf("post-%s final gate dispatch called=%t err=%v, want stale-claim conflict with no sender callback", action, dispatched, err)
	}
	if action == "update" {
		if _, err := service.Cancel(ctx, creator, fixture.sharedSpaceID, schedule.ID); err != nil {
			t.Fatalf("cancel updated safety-fixture schedule: %v", err)
		}
	}
}

func claimFinalGateDelivery(t *testing.T, testContext scheduledMessageTestContext, scheduleID string, due time.Time, action string) *domainscheduledmessage.Delivery {
	t.Helper()
	claimed, err := testContext.repository.ClaimDue(testContext.ctx, due.Add(time.Second), 2*time.Minute, 10)
	if err != nil {
		t.Fatalf("claim final-gate %s delivery: %v", action, err)
	}
	var claim *domainscheduledmessage.Delivery
	for i := range claimed {
		if claimed[i].ScheduledMessageID == scheduleID {
			claim = &claimed[i]
			break
		}
	}
	if claim == nil || claim.LeaseToken == nil {
		t.Fatalf("claim final-gate %s row missing from actual PostgreSQL claims", action)
	}
	return claim
}

func applyFinalGateMutation(t *testing.T, testContext scheduledMessageTestContext, scheduleID string, due time.Time, action string) {
	t.Helper()
	ctx, service, creator, recipient := testContext.ctx, testContext.service, testContext.daddy, testContext.member
	spaceID := testContext.fixture.sharedSpaceID
	switch action {
	case "cancel":
		if _, err := service.Cancel(ctx, creator, spaceID, scheduleID); err != nil {
			t.Fatalf("cancel claimed schedule before dispatch: %v", err)
		}
	case "unsubscribe":
		if _, err := service.Unsubscribe(ctx, recipient, spaceID, scheduleID); err != nil {
			t.Fatalf("unsubscribe claimed schedule before dispatch: %v", err)
		}
	case "update":
		newStart := due.Add(time.Hour)
		if _, err := service.Update(ctx, creator, spaceID, scheduleID, dto.ScheduledMessageUpdateInput{Content: stringPointer("updated before dispatch"), StartAt: &newStart}); err != nil {
			t.Fatalf("update claimed schedule before dispatch: %v", err)
		}
	default:
		t.Fatalf("unknown final-gate action %q", action)
	}
}

func assertConcurrentAndStaleClaims(t *testing.T, testContext scheduledMessageTestContext) {
	t.Helper()
	ctx, db, repository, service := testContext.ctx, testContext.db, testContext.repository, testContext.service
	creator, fixture := testContext.daddy, testContext.fixture
	due := time.Now().UTC().Add(45 * time.Minute)
	schedule := createScheduledMessage(t, ctx, service, creator, dto.ScheduledMessageCreateInput{
		Space: fixture.sharedSpaceID, Target: domainscheduledmessage.TargetDM, RecipientMemberID: stringPointer(fixture.sharedMemberIDs["Mommy"]),
		ContentMode: domainscheduledmessage.ContentModeFixed, Content: "concurrent claim fixture", Recurrence: domainscheduledmessage.RecurrenceOnce,
		Timezone: "UTC", StartAt: due,
	})
	claimAt := due.Add(time.Second)
	claims := claimScheduleConcurrently(t, testContext, schedule.ID, claimAt)
	if len(claims) != 1 || claims[0].LeaseToken == nil {
		t.Fatalf("concurrent SQL lock produced %d claims for one delivery, want exactly one", len(claims))
	}
	first := claims[0]
	if err := db.Model(&domainscheduledmessage.Delivery{}).Where("id = ?", first.ID).
		Update("lease_until", claimAt.Add(-time.Second)).Error; err != nil {
		t.Fatalf("expire first PostgreSQL claim: %v", err)
	}
	secondClaims, err := repository.ClaimDue(ctx, claimAt.Add(time.Second), 2*time.Minute, 1)
	if err != nil || len(secondClaims) != 1 || secondClaims[0].LeaseToken == nil {
		t.Fatalf("reclaim stale PostgreSQL lease: rows=%d err=%v", len(secondClaims), err)
	}
	if *first.LeaseToken == *secondClaims[0].LeaseToken {
		t.Fatal("stale lease reclaim reused the previous token")
	}
	if err := repository.ReserveDeliveryAttempt(ctx, &first, claimAt.Add(time.Second), 3); !errors.Is(err, interfacescheduledmessage.ErrClaimConflict) {
		t.Fatalf("stale token delivery attempt error=%v, want claim conflict", err)
	}
	if _, err := service.Cancel(ctx, creator, fixture.sharedSpaceID, schedule.ID); err != nil {
		t.Fatalf("cancel stale-claim fixture: %v", err)
	}
}

func claimScheduleConcurrently(t *testing.T, testContext scheduledMessageTestContext, scheduleID string, claimAt time.Time) []domainscheduledmessage.Delivery {
	t.Helper()
	type claimResult struct {
		rows []domainscheduledmessage.Delivery
		err  error
	}
	results := make(chan claimResult, 2)
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			rows, err := testContext.repository.ClaimDue(testContext.ctx, claimAt, 2*time.Minute, 1)
			results <- claimResult{rows: rows, err: err}
		}()
	}
	workers.Wait()
	close(results)
	claims := make([]domainscheduledmessage.Delivery, 0, 2)
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent PostgreSQL claim: %v", result.err)
		}
		for _, row := range result.rows {
			if row.ScheduledMessageID == scheduleID {
				claims = append(claims, row)
			}
		}
	}
	return claims
}

func assertScheduledMessageDeliveryRetryTimeConstraint(t *testing.T, db *gorm.DB, fixture scheduledMessageFixture) {
	t.Helper()
	startAt := time.Now().UTC().Add(time.Hour)
	scheduleID := utils.CreateUUID()
	if err := db.Exec(`
		INSERT INTO scheduled_messages (
			id, space_id, created_by_member_id, target, recipient_member_id,
			content_mode, content, recurrence, timezone, start_at, next_scheduled_at, status
		) VALUES (?, ?, ?, 'DM', ?, 'FIXED', 'constraint fixture', 'ONCE', 'UTC', ?, ?, 'ACTIVE')
	`, scheduleID, fixture.sharedSpaceID, fixture.sharedMemberIDs["Daddy"], fixture.sharedMemberIDs["Member"], startAt, startAt).Error; err != nil {
		t.Fatalf("create retry-time constraint fixture: %v", err)
	}

	insertDelivery := `
		INSERT INTO scheduled_message_deliveries (
			id, scheduled_message_id, scheduled_at, next_attempt_at, status, sent_at
		) VALUES (?, ?, ?, NULL, ?, ?)
	`
	if err := db.Exec(insertDelivery, utils.CreateUUID(), scheduleID, startAt.Add(-time.Minute), "PENDING", nil).Error; err == nil {
		t.Fatal("PENDING delivery with a NULL next_attempt_at was accepted")
	}

	terminalStatuses := []struct {
		status string
		sentAt *time.Time
	}{
		{status: "SENT", sentAt: &startAt},
		{status: "FAILED"},
		{status: "SKIPPED"},
		{status: "CANCELLED"},
	}
	for index, terminal := range terminalStatuses {
		if err := db.Exec(insertDelivery, utils.CreateUUID(), scheduleID, startAt.Add(time.Duration(index+1)*time.Minute), terminal.status, terminal.sentAt).Error; err != nil {
			t.Fatalf("insert %s delivery with NULL next_attempt_at: %v", terminal.status, err)
		}
	}
}

func createScheduledMessage(t *testing.T, ctx context.Context, service interfacescheduledmessage.Service, actor domainidentity.ActorContext, input dto.ScheduledMessageCreateInput) *domainscheduledmessage.Schedule {
	t.Helper()
	schedule, err := service.Create(ctx, actor, input)
	if err != nil {
		t.Fatalf("create scheduled message target=%s content_mode=%s: %v", input.Target, input.ContentMode, err)
	}
	return schedule
}

func testScheduledMessageConfig() config.ScheduledMessageConfig {
	return config.ScheduledMessageConfig{
		Enabled: true,
		Scheduler: config.ReminderSchedulerConfig{
			WhatsAppBridgeURL: "http://127.0.0.1:8081", Interval: 2 * time.Minute, BatchSize: 50, Lease: 2 * time.Minute,
			DatabaseFallbackInterval: time.Minute, IndexReconcileInterval: time.Hour, IndexBatchSize: 1000,
		},
		GeneratorModel: "scheduled-messages", GeneratorTimeout: 30 * time.Second, MaxResponseBytes: 65536,
		MaxOutputBytes: 2000, GenerationMaxRetries: 3, GenerationRetryBackoff: time.Minute,
		DeliveryMaxRetries: 3, DeliveryRetryBackoff: time.Minute, Lease: 2 * time.Minute,
		BatchSize: 10, Concurrency: 10, MaxTextBytes: 2000, MaxListRows: 100,
	}
}

func stringPointer(value string) *string { return &value }

func containsScheduledMessage(rows []dto.ScheduledMessageOutput, id string) bool {
	for _, row := range rows {
		if row.ID == id {
			return true
		}
	}
	return false
}

func containsString(rows []string, value string) bool {
	for _, row := range rows {
		if row == value {
			return true
		}
	}
	return false
}

type recordingScheduledMessageSender struct {
	mu      sync.Mutex
	targets []string
	bodies  []string
}

func (s *recordingScheduledMessageSender) Send(_ context.Context, target domainreminder.DeliveryTarget, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = append(s.targets, target.Target)
	s.bodies = append(s.bodies, body)
	return nil
}

func (s *recordingScheduledMessageSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.targets)
}

func (s *recordingScheduledMessageSender) targetsAfter(offset int) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if offset > len(s.targets) {
		return nil
	}
	return append([]string(nil), s.targets[offset:]...)
}
