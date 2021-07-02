package aws

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/acctest"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

func TestAccAWSDbClusterRoleAssociation_basic(t *testing.T) {
	var dbClusterRole1 rds.DBClusterRole
	rName := acctest.RandomWithPrefix("tf-acc-test")
	dbClusterResourceName := "aws_rds_cluster.test"
	iamRoleResourceName := "aws_iam_role.test"
	resourceName := "aws_db_cluster_role_association.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		ErrorCheck:   testAccErrorCheck(t, rds.EndpointsID),
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSDbClusterRoleAssociationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSDbClusterRoleAssociationConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSDbClusterRoleAssociationExists(resourceName, &dbClusterRole1),
					resource.TestCheckResourceAttrPair(resourceName, "db_cluster_identifier", dbClusterResourceName, "id"),
					resource.TestCheckResourceAttr(resourceName, "feature_name", "s3Import"),
					resource.TestCheckResourceAttrPair(resourceName, "role_arn", iamRoleResourceName, "arn"),
				),
			},
			{
				ResourceName:      resourceName,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccAWSDbClusterRoleAssociation_disappears(t *testing.T) {
	var dbCluster1 rds.DBCluster
	var dbClusterRole1 rds.DBClusterRole
	rName := acctest.RandomWithPrefix("tf-acc-test")
	dbClusterResourceName := "aws_rds_cluster.test"
	resourceName := "aws_db_cluster_role_association.test"

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		ErrorCheck:   testAccErrorCheck(t, rds.EndpointsID),
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckAWSDbClusterRoleAssociationDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccAWSDbClusterRoleAssociationConfig(rName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckAWSClusterExists(dbClusterResourceName, &dbCluster1),
					testAccCheckAWSDbClusterRoleAssociationExists(resourceName, &dbClusterRole1),
					testAccCheckAWSDbClusterRoleAssociationDisappears(&dbCluster1, &dbClusterRole1),
				),
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func testAccCheckAWSDbClusterRoleAssociationExists(resourceName string, dbClusterRole *rds.DBClusterRole) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]

		if !ok {
			return fmt.Errorf("Resource not found: %s", resourceName)
		}

		dbClusterIdentifier, roleArn, err := resourceAwsDbClusterRoleAssociationDecodeID(rs.Primary.ID)

		if err != nil {
			return fmt.Errorf("error reading resource ID: %s", err)
		}

		conn := testAccProvider.Meta().(*AWSClient).rdsconn

		role, err := rdsDescribeDbClusterRole(conn, dbClusterIdentifier, roleArn)

		if err != nil {
			return err
		}

		if role == nil {
			return fmt.Errorf("RDS DB Cluster IAM Role Association not found")
		}

		if aws.StringValue(role.Status) != "ACTIVE" {
			return fmt.Errorf("RDS DB Cluster (%s) IAM Role (%s) association exists in non-ACTIVE (%s) state", dbClusterIdentifier, roleArn, aws.StringValue(role.Status))
		}

		*dbClusterRole = *role

		return nil
	}
}

func testAccCheckAWSDbClusterRoleAssociationDestroy(s *terraform.State) error {
	conn := testAccProvider.Meta().(*AWSClient).rdsconn

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "aws_db_cluster_role_association" {
			continue
		}

		dbClusterIdentifier, roleArn, err := resourceAwsDbClusterRoleAssociationDecodeID(rs.Primary.ID)

		if err != nil {
			return fmt.Errorf("error reading resource ID: %s", err)
		}

		dbClusterRole, err := rdsDescribeDbClusterRole(conn, dbClusterIdentifier, roleArn)

		if isAWSErr(err, rds.ErrCodeDBClusterNotFoundFault, "") {
			continue
		}

		if err != nil {
			return err
		}

		if dbClusterRole == nil {
			continue
		}

		return fmt.Errorf("RDS DB Cluster (%s) IAM Role (%s) association still exists in non-deleted (%s) state", dbClusterIdentifier, roleArn, aws.StringValue(dbClusterRole.Status))
	}

	return nil
}

func testAccCheckAWSDbClusterRoleAssociationDisappears(dbCluster *rds.DBCluster, dbClusterRole *rds.DBClusterRole) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		conn := testAccProvider.Meta().(*AWSClient).rdsconn

		input := &rds.RemoveRoleFromDBClusterInput{
			DBClusterIdentifier: dbCluster.DBClusterIdentifier,
			FeatureName:         dbClusterRole.FeatureName,
			RoleArn:             dbClusterRole.RoleArn,
		}

		_, err := conn.RemoveRoleFromDBCluster(input)

		if err != nil {
			return err
		}

		return waitForRdsDbClusterRoleDisassociation(conn, aws.StringValue(dbCluster.DBClusterIdentifier), aws.StringValue(dbClusterRole.RoleArn))
	}
}

func testAccAWSDbClusterRoleAssociationConfig(rName string) string {
	return composeConfig(testAccAvailableAZsNoOptInConfig(), fmt.Sprintf(`
data "aws_iam_policy_document" "rds_assume_role_policy" {
  statement {
    actions = ["sts:AssumeRole"]
    effect  = "Allow"

    principals {
      identifiers = ["rds.amazonaws.com"]
      type        = "Service"
    }
  }
}

resource "aws_iam_role" "test" {
  assume_role_policy = data.aws_iam_policy_document.rds_assume_role_policy.json
  name               = %[1]q
}

resource "aws_rds_cluster" "test" {
  cluster_identifier      = %[1]q
  engine                  = "aurora-postgresql"
  availability_zones      = [data.aws_availability_zones.available.names[0], data.aws_availability_zones.available.names[1], data.aws_availability_zones.available.names[2]]
  database_name           = "mydb"
  master_username         = "foo"
  master_password         = "foobarfoobarfoobar"
  backup_retention_period = 5
  preferred_backup_window = "07:00-09:00"
  skip_final_snapshot     = true
}

resource "aws_db_cluster_role_association" "test" {
  db_cluster_identifier = aws_rds_cluster.test.id
  feature_name          = "s3Import"
  role_arn              = aws_iam_role.test.arn
}
`, rName))
}
