package integration

import (
	"github.com/greenplum-db/gpbackup/backup"
	"github.com/greenplum-db/gpbackup/testutils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("backup integration tests", func() {
	BeforeEach(func() {
		testutils.SetupTestLogger()
	})
	Describe("GetSessionGUCs", func() {
		It("returns a slice of values for session level GUCs", func() {
			/*
			 * We shouldn't need to run any setup queries, because we're using
			 * the default values for GPDB 5.
			 */
			results := backup.GetSessionGUCs(connection)
			Expect(results.ClientEncoding).To(Equal("UTF8"))
			Expect(results.StdConformingStrings).To(Equal("on"))
			Expect(results.DefaultWithOids).To(Equal("off"))
		})
	})
	Describe("GetDatabaseGUCs", func() {
		It("returns a slice of values for database level GUCs", func() {
			testutils.AssertQueryRuns(connection, "ALTER DATABASE testdb SET default_with_oids TO true")
			defer testutils.AssertQueryRuns(connection, "ALTER DATABASE testdb SET default_with_oids TO false")
			testutils.AssertQueryRuns(connection, "ALTER DATABASE testdb SET search_path TO public,pg_catalog")
			defer testutils.AssertQueryRuns(connection, "ALTER DATABASE testdb SET search_path TO pg_catalog,public")
			results := backup.GetDatabaseGUCs(connection)
			Expect(len(results)).To(Equal(2))
			Expect(results[0]).To(Equal("SET default_with_oids TO true"))
			Expect(results[1]).To(Equal("SET search_path TO public, pg_catalog"))
		})
	})
	Describe("GetResourceQueues", func() {
		It("returns a slice for a resource queue with only ACTIVE_STATEMENTS", func() {
			testutils.AssertQueryRuns(connection, `CREATE RESOURCE QUEUE "statementsQueue" WITH (ACTIVE_STATEMENTS=7);`)
			defer testutils.AssertQueryRuns(connection, `DROP RESOURCE QUEUE "statementsQueue"`)

			results := backup.GetResourceQueues(connection)

			statementsQueue := backup.ResourceQueue{1, "statementsQueue", 7, "-1.00", false, "0.00", "medium", "-1"}

			//Since resource queues are global, we can't be sure this is the only one
			for _, resultQueue := range results {
				if resultQueue.Name == "statementsQueue" {
					testutils.ExpectStructsToMatchExcluding(&statementsQueue, &resultQueue, "Oid")
					return
				}
			}
			Fail("Resource queue 'statementsQueue' was not found.")
		})
		It("returns a slice for a resource queue with only MAX_COST", func() {
			testutils.AssertQueryRuns(connection, `CREATE RESOURCE QUEUE "maxCostQueue" WITH (MAX_COST=32.8);`)
			defer testutils.AssertQueryRuns(connection, `DROP RESOURCE QUEUE "maxCostQueue"`)

			results := backup.GetResourceQueues(connection)

			maxCostQueue := backup.ResourceQueue{1, "maxCostQueue", -1, "32.80", false, "0.00", "medium", "-1"}

			for _, resultQueue := range results {
				if resultQueue.Name == "maxCostQueue" {
					testutils.ExpectStructsToMatchExcluding(&maxCostQueue, &resultQueue, "Oid")
					return
				}
			}
			Fail("Resource queue 'maxCostQueue' was not found.")
		})
		It("returns a slice for a resource queue with everything", func() {
			testutils.AssertQueryRuns(connection, `CREATE RESOURCE QUEUE "everyQueue" WITH (ACTIVE_STATEMENTS=7, MAX_COST=3e+4, COST_OVERCOMMIT=TRUE, MIN_COST=22.53, PRIORITY=LOW, MEMORY_LIMIT='2GB');`)
			defer testutils.AssertQueryRuns(connection, `DROP RESOURCE QUEUE "everyQueue"`)

			results := backup.GetResourceQueues(connection)

			everyQueue := backup.ResourceQueue{1, "everyQueue", 7, "30000.00", true, "22.53", "low", "2GB"}

			for _, resultQueue := range results {
				if resultQueue.Name == "everyQueue" {
					testutils.ExpectStructsToMatchExcluding(&everyQueue, &resultQueue, "Oid")
					return
				}
			}
			Fail("Resource queue 'everyQueue' was not found.")
		})

	})
	Describe("GetDatabaseRoles", func() {
		It("returns a role with default properties", func() {
			testutils.AssertQueryRuns(connection, "CREATE ROLE role1 SUPERUSER NOINHERIT")
			defer testutils.AssertQueryRuns(connection, "DROP ROLE role1")

			results := backup.GetRoles(connection)

			roleOid := testutils.OidFromObjectName(connection, "", "role1", backup.TYPE_ROLE)
			expectedRole := backup.Role{
				Oid:             roleOid,
				Name:            "role1",
				Super:           true,
				Inherit:         false,
				CreateRole:      false,
				CreateDB:        false,
				CanLogin:        false,
				ConnectionLimit: -1,
				Password:        "",
				ValidUntil:      "",
				ResQueue:        "pg_default",
				Createrexthttp:  false,
				Createrextgpfd:  false,
				Createwextgpfd:  false,
				Createrexthdfs:  false,
				Createwexthdfs:  false,
				TimeConstraints: nil,
			}

			for _, role := range results {
				if role.Name == "role1" {
					testutils.ExpectStructsToMatch(&expectedRole, role)
					return
				}
			}
			Fail("Role 'role1' was not found")
		})
		It("returns a role with all properties specified", func() {
			testutils.AssertQueryRuns(connection, "CREATE ROLE role1")
			defer testutils.AssertQueryRuns(connection, "DROP ROLE role1")
			testutils.AssertQueryRuns(connection, `
ALTER ROLE role1 WITH NOSUPERUSER INHERIT CREATEROLE CREATEDB LOGIN
CONNECTION LIMIT 4 PASSWORD 'swordfish' VALID UNTIL '2099-01-01 00:00:00-08'
CREATEEXTTABLE (protocol='http')
CREATEEXTTABLE (protocol='gpfdist', type='readable')
CREATEEXTTABLE (protocol='gpfdist', type='writable')
CREATEEXTTABLE (protocol='gphdfs', type='readable')
CREATEEXTTABLE (protocol='gphdfs', type='writable')`)
			testutils.AssertQueryRuns(connection, "ALTER ROLE role1 DENY BETWEEN DAY 'Sunday' TIME '1:30 PM' AND DAY 'Wednesday' TIME '14:30:00'")
			testutils.AssertQueryRuns(connection, "ALTER ROLE role1 DENY DAY 'Friday'")
			testutils.AssertQueryRuns(connection, "COMMENT ON ROLE role1 IS 'this is a role comment'")

			results := backup.GetRoles(connection)

			roleOid := testutils.OidFromObjectName(connection, "", "role1", backup.TYPE_ROLE)
			expectedRole := backup.Role{
				Oid:             roleOid,
				Name:            "role1",
				Super:           false,
				Inherit:         true,
				CreateRole:      true,
				CreateDB:        true,
				CanLogin:        true,
				ConnectionLimit: 4,
				Password:        "md5a8b2c77dfeba4705f29c094592eb3369",
				ValidUntil:      "2099-01-01 08:00:00-00",
				ResQueue:        "pg_default",
				Createrexthttp:  true,
				Createrextgpfd:  true,
				Createwextgpfd:  true,
				Createrexthdfs:  true,
				Createwexthdfs:  true,
				TimeConstraints: []backup.TimeConstraint{
					{
						Oid:       0,
						StartDay:  0,
						StartTime: "13:30:00",
						EndDay:    3,
						EndTime:   "14:30:00",
					}, {
						Oid:       0,
						StartDay:  5,
						StartTime: "00:00:00",
						EndDay:    5,
						EndTime:   "24:00:00",
					},
				},
			}

			for _, role := range results {
				if role.Name == "role1" {
					testutils.ExpectStructsToMatchExcluding(&expectedRole, role, "TimeConstraints.Oid")
					return
				}
			}
			Fail("Role 'role1' was not found")
		})
	})
	Describe("GetRoleMembers", func() {
		BeforeEach(func() {
			testutils.AssertQueryRuns(connection, `CREATE ROLE usergroup`)
			testutils.AssertQueryRuns(connection, `CREATE ROLE testuser`)
		})
		AfterEach(func() {
			defer testutils.AssertQueryRuns(connection, `DROP ROLE usergroup`)
			defer testutils.AssertQueryRuns(connection, `DROP ROLE testuser`)
		})
		It("returns a role without ADMIN OPTION", func() {
			testutils.AssertQueryRuns(connection, "GRANT usergroup TO testuser")
			expectedRoleMember := backup.RoleMember{"usergroup", "testuser", "testrole", false}

			roleMembers := backup.GetRoleMembers(connection)

			for _, roleMember := range roleMembers {
				if roleMember.Role == "usergroup" {
					testutils.ExpectStructsToMatch(&expectedRoleMember, &roleMember)
					return
				}
			}
			Fail("Role 'testuser' is not a member of role 'usergroup'")
		})
		It("returns a role WITH ADMIN OPTION", func() {
			testutils.AssertQueryRuns(connection, "GRANT usergroup TO testuser WITH ADMIN OPTION GRANTED BY testrole")
			expectedRoleMember := backup.RoleMember{"usergroup", "testuser", "testrole", true}

			roleMembers := backup.GetRoleMembers(connection)

			for _, roleMember := range roleMembers {
				if roleMember.Role == "usergroup" {
					testutils.ExpectStructsToMatch(&expectedRoleMember, &roleMember)
					return
				}
			}
			Fail("Role 'testuser' is not a member of role 'usergroup'")
		})
	})
	Describe("GetTablespaces", func() {
		It("returns a tablespace", func() {
			testutils.AssertQueryRuns(connection, "CREATE TABLESPACE test_tablespace FILESPACE test_filespace")
			defer testutils.AssertQueryRuns(connection, "DROP TABLESPACE test_tablespace")
			expectedTablespace := backup.Tablespace{0, "test_tablespace", "test_filespace"}

			resultTablespaces := backup.GetTablespaces(connection)

			for _, tablespace := range resultTablespaces {
				if tablespace.Tablespace == "test_tablespace" {
					testutils.ExpectStructsToMatchExcluding(&expectedTablespace, &tablespace, "Oid")
					return
				}
			}
			Fail("Tablespace 'test_tablespace' was not created")
		})
	})
})