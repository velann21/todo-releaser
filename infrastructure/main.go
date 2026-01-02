package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/rds"
	"github.com/pulumi/pulumi-command/sdk/go/command/local"
	"github.com/pulumi/pulumi-random/sdk/v4/go/random"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func ensureSSHKey(keyPath string) error {
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		// Generate SSH key
		cmd := exec.Command("ssh-keygen", "-t", "rsa", "-b", "4096", "-f", keyPath, "-N", "")
		if err := cmd.Run(); err != nil {
			return err
		}
		// Ensure correct permissions
		if err := os.Chmod(keyPath, 0400); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// 0. Ensure SSH Key Exists
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		privateKeyPath := filepath.Join(homeDir, ".ssh", "todo-key.pem")
		publicKeyPath := privateKeyPath + ".pub"

		if err := ensureSSHKey(privateKeyPath); err != nil {
			return err
		}

		publicKeyContent, err := os.ReadFile(publicKeyPath)
		if err != nil {
			return err
		}

		// Create AWS Key Pair
		keyPair, err := ec2.NewKeyPair(ctx, "todo-key-pair", &ec2.KeyPairArgs{
			PublicKey: pulumi.String(string(publicKeyContent)),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-key-pair"),
			},
		})
		if err != nil {
			return err
		}

		// 1. Create a VPC
		vpc, err := ec2.NewVpc(ctx, "todo-vpc", &ec2.VpcArgs{
			CidrBlock:          pulumi.String("10.0.0.0/16"),
			EnableDnsHostnames: pulumi.Bool(true),
			EnableDnsSupport:   pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-vpc"),
			},
		})
		if err != nil {
			return err
		}

		// Create an Internet Gateway
		igw, err := ec2.NewInternetGateway(ctx, "todo-igw", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-igw"),
			},
		})
		if err != nil {
			return err
		}

		// 2. Subnets
		// Public Subnet (for EC2)
		publicSubnet, err := ec2.NewSubnet(ctx, "todo-public-subnet-1", &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String("10.0.1.0/24"),
			MapPublicIpOnLaunch: pulumi.Bool(true),
			AvailabilityZone:    pulumi.String("eu-west-1a"),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-public-subnet-1"),
			},
		})
		if err != nil {
			return err
		}

		// Private Subnet 1 (for RDS)
		privateSubnet1, err := ec2.NewSubnet(ctx, "todo-private-subnet-1", &ec2.SubnetArgs{
			VpcId:            vpc.ID(),
			CidrBlock:        pulumi.String("10.0.2.0/24"),
			AvailabilityZone: pulumi.String("eu-west-1a"),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-private-subnet-1"),
			},
		})
		if err != nil {
			return err
		}

		// Private Subnet 2 (for RDS HA)
		privateSubnet2, err := ec2.NewSubnet(ctx, "todo-private-subnet-2", &ec2.SubnetArgs{
			VpcId:            vpc.ID(),
			CidrBlock:        pulumi.String("10.0.3.0/24"),
			AvailabilityZone: pulumi.String("eu-west-1b"),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-private-subnet-2"),
			},
		})
		if err != nil {
			return err
		}

		// Route Table for Public Subnet
		publicRt, err := ec2.NewRouteTable(ctx, "todo-public-rt", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Routes: ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					CidrBlock: pulumi.String("0.0.0.0/0"),
					GatewayId: igw.ID(),
				},
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-public-rt"),
			},
		})
		if err != nil {
			return err
		}

		// Associate Public Route Table with Public Subnet
		_, err = ec2.NewRouteTableAssociation(ctx, "todo-public-rta", &ec2.RouteTableAssociationArgs{
			SubnetId:     publicSubnet.ID(),
			RouteTableId: publicRt.ID(),
		})
		if err != nil {
			return err
		}

		// 3. Security Groups
		// Web SG for EC2
		webSg, err := ec2.NewSecurityGroup(ctx, "todo-web-sg", &ec2.SecurityGroupArgs{
			VpcId:       vpc.ID(),
			Description: pulumi.String("Allow HTTP/HTTPS and SSH"),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(80),
					ToPort:      pulumi.Int(80),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("HTTP"),
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(443),
					ToPort:      pulumi.Int(443),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("HTTPS"),
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(3000),
					ToPort:      pulumi.Int(3000),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")},
					Description: pulumi.String("Frontend"),
				},
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(22),
					ToPort:      pulumi.Int(22),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")}, // Restrict this in production!
					Description: pulumi.String("SSH"),
				},
			},
			Egress: ec2.SecurityGroupEgressArray{
				&ec2.SecurityGroupEgressArgs{
					Protocol:   pulumi.String("-1"),
					FromPort:   pulumi.Int(0),
					ToPort:     pulumi.Int(0),
					CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
				},
			},
		})
		if err != nil {
			return err
		}

		// DB SG
		dbSg, err := ec2.NewSecurityGroup(ctx, "todo-db-sg", &ec2.SecurityGroupArgs{
			VpcId:       vpc.ID(),
			Description: pulumi.String("Allow PostgreSQL from Web SG"),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					Protocol:       pulumi.String("tcp"),
					FromPort:       pulumi.Int(5432),
					ToPort:         pulumi.Int(5432),
					SecurityGroups: pulumi.StringArray{webSg.ID()},
				},
			},
		})
		if err != nil {
			return err
		}

		// 4. RDS Aurora PostgreSQL
		dbSubnetGroup, err := rds.NewSubnetGroup(ctx, "todo-db-subnet-group", &rds.SubnetGroupArgs{
			SubnetIds: pulumi.StringArray{privateSubnet1.ID(), privateSubnet2.ID()},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-db-subnet-group"),
			},
		})
		if err != nil {
			return err
		}

		// Config
		conf := config.New(ctx, "")
		dbUsername := conf.Get("dbUsername")
		if dbUsername == "" {
			dbUsername = "postgres"
		}
		if dbUsername == "" {
			dbUsername = "postgres"
		}
		frontendImage := conf.Get("frontendImage")
		if frontendImage == "" {
			frontendImage = "singaravelan21/todo-frontend"
		}
		frontendVersion := conf.Get("frontendVersion")
		if frontendVersion == "" {
			frontendVersion = "latest"
		}
		backendImage := conf.Get("backendImage")
		if backendImage == "" {
			backendImage = "singaravelan21/todo-backend"
		}
		backendVersion := conf.Get("backendVersion")
		if backendVersion == "" {
			backendVersion = "latest"
		}
		dockerUsername := conf.Require("dockerUsername")
		dockerPassword := conf.RequireSecret("dockerPassword")

		fmt.Println("Docker Username: ", dockerUsername)
		fmt.Println("Docker Password: ", dockerPassword)
		// Generate Random Password
		dbPassword, err := random.NewRandomPassword(ctx, "db-password", &random.RandomPasswordArgs{
			Length:          pulumi.Int(16),
			Special:         pulumi.Bool(true),
			OverrideSpecial: pulumi.String("_%"),
		})
		if err != nil {
			return err
		}

		cluster, err := rds.NewCluster(ctx, "todo-db-cluster", &rds.ClusterArgs{
			Engine:              rds.EngineTypeAuroraPostgresql,
			EngineMode:          pulumi.String("provisioned"),
			EngineVersion:       pulumi.String("15.6"),
			DatabaseName:        pulumi.String("todoapp"),
			MasterUsername:      pulumi.String(dbUsername),
			MasterPassword:      dbPassword.Result,
			SkipFinalSnapshot:   pulumi.Bool(true),
			VpcSecurityGroupIds: pulumi.StringArray{dbSg.ID()},
			DbSubnetGroupName:   dbSubnetGroup.Name,
			Serverlessv2ScalingConfiguration: &rds.ClusterServerlessv2ScalingConfigurationArgs{
				MinCapacity: pulumi.Float64(0.5),
				MaxCapacity: pulumi.Float64(1.0),
			},
		})
		if err != nil {
			return err
		}

		_, err = rds.NewClusterInstance(ctx, "todo-db-instance", &rds.ClusterInstanceArgs{
			ClusterIdentifier: cluster.ID(),
			InstanceClass:     pulumi.String("db.serverless"),
			Engine:            rds.EngineTypeAuroraPostgresql,
			EngineVersion:     cluster.EngineVersion,
		})
		if err != nil {
			return err
		}

		// 5. EC2 Instance (Public Subnet, No UserData)
		ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
			MostRecent: pulumi.BoolRef(true),
			Owners:     []string{"amazon"},
			Filters: []ec2.GetAmiFilter{
				{
					Name:   "name",
					Values: []string{"al2023-ami-2023.*-x86_64"},
				},
			},
		})
		if err != nil {
			return err
		}

		server, err := ec2.NewInstance(ctx, "todo-server-v2", &ec2.InstanceArgs{
			InstanceType:        pulumi.String("t3.micro"),
			VpcSecurityGroupIds: pulumi.StringArray{webSg.ID()},
			Ami:                 pulumi.String(ami.Id),
			SubnetId:            publicSubnet.ID(),
			KeyName:             keyPair.KeyName,
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-server-v2"),
			},
		})
		if err != nil {
			return err
		}

		// Generate Django Secret Key
		djangoSecret, err := random.NewRandomPassword(ctx, "django-secret", &random.RandomPasswordArgs{
			Length:  pulumi.Int(50),
			Special: pulumi.Bool(true),
		})
		if err != nil {
			return err
		}

		// 6. Run Ansible Playbook
		_, err = local.NewCommand(ctx, "run-ansible", &local.CommandArgs{
			Create: pulumi.Sprintf("sleep 60; ANSIBLE_HOST_KEY_CHECKING=False ansible-playbook -vvv -u ec2-user --private-key %s -i '%s,' -e 'database_url=postgres://%s:%s@%s/%s' -e 'secret_key=%s' -e 'frontend_image=%s' -e 'frontend_version=%s' -e 'backend_image=%s' -e 'backend_version=%s' -e 'docker_username=%s' -e 'docker_password=%s' ansible/playbook.yml",
				privateKeyPath,
				server.PublicIp,
				cluster.MasterUsername,
				dbPassword.Result,
				cluster.Endpoint,
				cluster.DatabaseName,
				djangoSecret.Result,
				pulumi.String(frontendImage),
				pulumi.String(frontendVersion),
				pulumi.String(backendImage),
				pulumi.String(backendVersion),
				dockerUsername,
				dockerPassword,
			),
			Triggers: pulumi.Array{
				server.PublicIp,
				djangoSecret.Result,
				dockerPassword,
			},
		}, pulumi.DependsOn([]pulumi.Resource{server}))
		if err != nil {
			return err
		}

		// Outputs
		ctx.Export("publicIp", server.PublicIp)
		ctx.Export("publicHostName", server.PublicDns)
		ctx.Export("dbEndpoint", cluster.Endpoint)
		ctx.Export("dbUsername", cluster.MasterUsername)
		ctx.Export("dbPassword", cluster.MasterPassword)
		ctx.Export("dbConnectionString", pulumi.Sprintf("postgres://%s:%s@%s/%s",
			cluster.MasterUsername,
			dbPassword.Result,
			cluster.Endpoint,
			cluster.DatabaseName,
		))

		return nil
	})
}
