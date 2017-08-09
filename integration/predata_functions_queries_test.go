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
	Describe("GetFunctionDefinitions", func() {
		It("returns a slice of function definitions", func() {
			testutils.AssertQueryRuns(connection, `CREATE FUNCTION add(integer, integer) RETURNS integer
AS 'SELECT $1 + $2'
LANGUAGE SQL`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION add(integer, integer)")
			testutils.AssertQueryRuns(connection, `
CREATE FUNCTION append(integer, integer) RETURNS SETOF record
AS 'SELECT ($1, $2)'
LANGUAGE SQL
SECURITY DEFINER
STRICT
STABLE
COST 200
ROWS 200
SET search_path = pg_temp
MODIFIES SQL DATA
`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION append(integer, integer)")
			testutils.AssertQueryRuns(connection, "COMMENT ON FUNCTION append(integer, integer) IS 'this is a function comment'")

			results := backup.GetFunctions(connection)

			addFunction := backup.Function{
				SchemaName: "public", FunctionName: "add", ReturnsSet: false, FunctionBody: "SELECT $1 + $2",
				BinaryPath: "", Arguments: "integer, integer", IdentArgs: "integer, integer", ResultType: "integer",
				Volatility: "v", IsStrict: false, IsSecurityDefiner: false, Config: "", Cost: 100, NumRows: 0, DataAccess: "c",
				Language: "sql"}
			appendFunction := backup.Function{
				SchemaName: "public", FunctionName: "append", ReturnsSet: true, FunctionBody: "SELECT ($1, $2)",
				BinaryPath: "", Arguments: "integer, integer", IdentArgs: "integer, integer", ResultType: "SETOF record",
				Volatility: "s", IsStrict: true, IsSecurityDefiner: true, Config: "SET search_path TO pg_temp", Cost: 200,
				NumRows: 200, DataAccess: "m", Language: "sql"}

			Expect(len(results)).To(Equal(2))
			testutils.ExpectStructsToMatchExcluding(&results[0], &addFunction, "Oid")
			testutils.ExpectStructsToMatchExcluding(&results[1], &appendFunction, "Oid")
		})
	})
	Describe("GetAggregateDefinitions", func() {
		It("returns a slice of aggregate definitions", func() {
			testutils.AssertQueryRuns(connection, `
CREATE FUNCTION mysfunc_accum(numeric, numeric, numeric)
   RETURNS numeric
   AS 'select $1 + $2 + $3'
   LANGUAGE SQL
   IMMUTABLE
   RETURNS NULL ON NULL INPUT;
`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION mysfunc_accum(numeric, numeric, numeric)")
			testutils.AssertQueryRuns(connection, `
CREATE FUNCTION mypre_accum(numeric, numeric)
   RETURNS numeric
   AS 'select $1 + $2'
   LANGUAGE SQL
   IMMUTABLE
   RETURNS NULL ON NULL INPUT;
`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION mypre_accum(numeric, numeric)")
			testutils.AssertQueryRuns(connection, `
CREATE AGGREGATE agg_prefunc(numeric, numeric) (
	SFUNC = mysfunc_accum,
	STYPE = numeric,
	PREFUNC = mypre_accum,
	INITCOND = 0 );
`)
			defer testutils.AssertQueryRuns(connection, "DROP AGGREGATE agg_prefunc(numeric, numeric)")

			transitionOid := testutils.OidFromObjectName(connection, "public", "mysfunc_accum", backup.TYPE_FUNCTION)
			prelimOid := testutils.OidFromObjectName(connection, "public", "mypre_accum", backup.TYPE_FUNCTION)

			result := backup.GetAggregates(connection)

			aggregateDef := backup.Aggregate{
				SchemaName: "public", AggregateName: "agg_prefunc", Arguments: "numeric, numeric",
				IdentArgs: "numeric, numeric", TransitionFunction: transitionOid, PreliminaryFunction: prelimOid,
				FinalFunction: 0, SortOperator: 0, TransitionDataType: "numeric", InitialValue: "0", IsOrdered: false,
			}

			Expect(len(result)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&result[0], &aggregateDef, "Oid")
		})
	})
	Describe("GetFunctionOidToInfoMap", func() {
		It("returns map containing function information", func() {
			result := backup.GetFunctionOidToInfoMap(connection)
			initialLength := len(result)
			testutils.AssertQueryRuns(connection, `CREATE FUNCTION add(integer, integer) RETURNS integer
AS 'SELECT $1 + $2'
LANGUAGE SQL`)
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION add(integer, integer)")

			result = backup.GetFunctionOidToInfoMap(connection)
			oid := testutils.OidFromObjectName(connection, "public", "add", backup.TYPE_FUNCTION)
			Expect(len(result)).To(Equal(initialLength + 1))
			Expect(result[oid].QualifiedName).To(Equal("public.add"))
			Expect(result[oid].Arguments).To(Equal("integer, integer"))
			Expect(result[oid].IsInternal).To(BeFalse())
		})
		It("returns a map containing an internal function", func() {
			result := backup.GetFunctionOidToInfoMap(connection)

			oid := testutils.OidFromObjectName(connection, "pg_catalog", "boolin", backup.TYPE_FUNCTION)
			Expect(result[oid].QualifiedName).To(Equal("pg_catalog.boolin"))
			Expect(result[oid].IsInternal).To(BeTrue())
		})
	})
	Describe("GetCastDefinitions", func() {
		It("returns a slice for a basic cast with a function", func() {
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION casttoint(text) RETURNS integer STRICT IMMUTABLE LANGUAGE SQL AS 'SELECT cast($1 as integer);'")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION casttoint(text)")
			testutils.AssertQueryRuns(connection, "CREATE CAST (text AS int4) WITH FUNCTION casttoint(text) AS ASSIGNMENT")
			defer testutils.AssertQueryRuns(connection, "DROP CAST (text AS int4)")

			results := backup.GetCasts(connection)

			castDef := backup.Cast{0, "pg_catalog.text", "pg_catalog.int4", "public", "casttoint", "text", "a"}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&castDef, &results[0], "Oid")
		})
		It("returns a slice for a basic cast without a function", func() {
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION cast_in(cstring) RETURNS casttesttype AS $$textin$$ LANGUAGE internal STRICT NO SQL")
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION cast_out(casttesttype) RETURNS cstring AS $$textout$$ LANGUAGE internal STRICT NO SQL")
			testutils.AssertQueryRuns(connection, "CREATE TYPE casttesttype (INTERNALLENGTH = variable, INPUT = cast_in, OUTPUT = cast_out)")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE casttesttype CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE CAST (text AS public.casttesttype) WITHOUT FUNCTION AS IMPLICIT")
			defer testutils.AssertQueryRuns(connection, "DROP CAST (text AS public.casttesttype)")

			results := backup.GetCasts(connection)

			castDef := backup.Cast{0, "pg_catalog.text", "public.casttesttype", "", "", "", "i"}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&castDef, &results[0], "Oid")
		})
		It("returns a slice for a basic cast with comment", func() {
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION casttoint(text) RETURNS integer STRICT IMMUTABLE LANGUAGE SQL AS 'SELECT cast($1 as integer);'")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION casttoint(text)")
			testutils.AssertQueryRuns(connection, "CREATE CAST (text AS integer) WITH FUNCTION casttoint(text) AS ASSIGNMENT")
			defer testutils.AssertQueryRuns(connection, "DROP CAST (text AS int4)")
			testutils.AssertQueryRuns(connection, "COMMENT ON CAST (text AS int4) IS 'this is a cast comment'")

			results := backup.GetCasts(connection)

			castDef := backup.Cast{1, "pg_catalog.text", "pg_catalog.int4", "public", "casttoint", "text", "a"}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&castDef, &results[0], "Oid")
		})
		It("returns a slice for a cast with source and target types in different schemas", func() {
			testutils.AssertQueryRuns(connection, "CREATE SCHEMA testschema1")
			defer testutils.AssertQueryRuns(connection, "DROP SCHEMA testschema1")
			testutils.AssertQueryRuns(connection, "CREATE SCHEMA testschema2")
			defer testutils.AssertQueryRuns(connection, "DROP SCHEMA testschema2")
			testutils.AssertQueryRuns(connection, "CREATE TYPE testschema1.casttesttype1 AS (t text)")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE testschema1.casttesttype1 CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE TYPE testschema2.casttesttype2 AS (t text)")
			defer testutils.AssertQueryRuns(connection, "DROP TYPE testschema2.casttesttype2 CASCADE")
			testutils.AssertQueryRuns(connection, "CREATE CAST (testschema1.casttesttype1 AS testschema2.casttesttype2) WITHOUT FUNCTION AS IMPLICIT")
			defer testutils.AssertQueryRuns(connection, "DROP CAST (testschema1.casttesttype1 AS testschema2.casttesttype2)")

			results := backup.GetCasts(connection)

			castDef := backup.Cast{0, "testschema1.casttesttype1", "testschema2.casttesttype2", "", "", "", "i"}

			Expect(len(results)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&castDef, &results[0], "Oid")
		})
	})
	Describe("GetProceduralLanguages", func() {
		It("returns a slice of procedural languages", func() {
			testutils.AssertQueryRuns(connection, "CREATE LANGUAGE plperl")
			defer testutils.AssertQueryRuns(connection, "DROP LANGUAGE plperl")

			pgsqlHandlerOid := testutils.OidFromObjectName(connection, "pg_catalog", "plpgsql_call_handler", backup.TYPE_FUNCTION)
			pgsqlInlineOid := testutils.OidFromObjectName(connection, "pg_catalog", "plpgsql_inline_handler", backup.TYPE_FUNCTION)
			pgsqlValidatorOid := testutils.OidFromObjectName(connection, "pg_catalog", "plpgsql_validator", backup.TYPE_FUNCTION)

			perlHandlerOid := testutils.OidFromObjectName(connection, "pg_catalog", "plperl_call_handler", backup.TYPE_FUNCTION)
			perlInlineOid := testutils.OidFromObjectName(connection, "pg_catalog", "plperl_inline_handler", backup.TYPE_FUNCTION)
			perlValidatorOid := testutils.OidFromObjectName(connection, "pg_catalog", "plperl_validator", backup.TYPE_FUNCTION)

			expectedPlpgsqlInfo := backup.ProceduralLanguage{0, "plpgsql", "testrole", true, true, pgsqlHandlerOid, pgsqlInlineOid, pgsqlValidatorOid}
			expectedPlperlInfo := backup.ProceduralLanguage{1, "plperl", "testrole", true, true, perlHandlerOid, perlInlineOid, perlValidatorOid}

			resultProcLangs := backup.GetProceduralLanguages(connection)

			Expect(len(resultProcLangs)).To(Equal(2))
			testutils.ExpectStructsToMatchExcluding(&expectedPlpgsqlInfo, &resultProcLangs[0], "Oid", "Owner")
			testutils.ExpectStructsToMatchExcluding(&expectedPlperlInfo, &resultProcLangs[1], "Oid", "Owner")
		})
	})
	Describe("GetConversions", func() {
		It("returns a slice of conversions", func() {
			testutils.AssertQueryRuns(connection, "CREATE CONVERSION testconv FOR 'LATIN1' TO 'MULE_INTERNAL' FROM latin1_to_mic")
			defer testutils.AssertQueryRuns(connection, "DROP CONVERSION testconv")

			expectedConversion := backup.Conversion{0, "public", "testconv", "LATIN1", "MULE_INTERNAL", "pg_catalog.latin1_to_mic", false}

			resultConversions := backup.GetConversions(connection)

			Expect(len(resultConversions)).To(Equal(1))
			testutils.ExpectStructsToMatchExcluding(&expectedConversion, &resultConversions[0], "Oid")
		})
	})
	Describe("ConstructFunctionDependencies", func() {
		BeforeEach(func() {
			testutils.AssertQueryRuns(connection, "CREATE TYPE composite_ints AS (one integer, two integer)")
		})
		AfterEach(func() {
			testutils.AssertQueryRuns(connection, "DROP TYPE composite_ints CASCADE")
		})
		It("constructs dependencies correctly for a function dependent on a user-defined type in the arguments", func() {
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION add(composite_ints) RETURNS integer STRICT IMMUTABLE LANGUAGE SQL AS 'SELECT ($1.one + $1.two);'")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION add(composite_ints)")

			allFunctions := backup.GetFunctions(connection)
			function := backup.Function{}
			for _, funct := range allFunctions {
				if funct.FunctionName == "add" {
					function = funct
					break
				}
			}
			functions := []backup.Function{function}

			functions = backup.ConstructFunctionDependencies(connection, functions)

			Expect(len(functions)).To(Equal(1))
			Expect(len(functions[0].DependsUpon)).To(Equal(1))
			Expect(functions[0].DependsUpon[0]).To(Equal("public.composite_ints"))
		})
		It("constructs dependencies correctly for a function dependent on a user-defined type in the return type", func() {
			testutils.AssertQueryRuns(connection, "CREATE FUNCTION compose(integer, integer) RETURNS composite_ints STRICT IMMUTABLE LANGUAGE PLPGSQL AS 'DECLARE comp composite_ints; BEGIN SELECT $1, $2 INTO comp; RETURN comp; END;';")
			defer testutils.AssertQueryRuns(connection, "DROP FUNCTION compose(integer, integer)")

			allFunctions := backup.GetFunctions(connection)
			function := backup.Function{}
			for _, funct := range allFunctions {
				if funct.FunctionName == "compose" {
					function = funct
					break
				}
			}
			functions := []backup.Function{function}

			functions = backup.ConstructFunctionDependencies(connection, functions)

			Expect(len(functions)).To(Equal(1))
			Expect(len(functions[0].DependsUpon)).To(Equal(1))
			Expect(functions[0].DependsUpon[0]).To(Equal("public.composite_ints"))
		})
	})
})