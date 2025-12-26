package main

import (
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/rds"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
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

		// Create a Public Subnet
		subnet, err := ec2.NewSubnet(ctx, "todo-subnet", &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String("10.0.1.0/24"),
			MapPublicIpOnLaunch: pulumi.Bool(true),
			AvailabilityZone:    pulumi.String("us-east-1a"), // Hardcoded for simplicity, should be dynamic
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-subnet"),
			},
		})
		if err != nil {
			return err
		}

		// Create Route Table
		rt, err := ec2.NewRouteTable(ctx, "todo-rt", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Routes: ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					CidrBlock: pulumi.String("0.0.0.0/0"),
					GatewayId: igw.ID(),
				},
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-rt"),
			},
		})
		if err != nil {
			return err
		}

		// Associate Route Table with Subnet
		_, err = ec2.NewRouteTableAssociation(ctx, "todo-rta", &ec2.RouteTableAssociationArgs{
			SubnetId:     subnet.ID(),
			RouteTableId: rt.ID(),
		})
		if err != nil {
			return err
		}

		// 2. Security Groups
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

		// 3. RDS Aurora PostgreSQL
		// Subnet Group for RDS (needs at least 2 AZs usually, creating a second subnet for this)
		subnet2, err := ec2.NewSubnet(ctx, "todo-subnet-2", &ec2.SubnetArgs{
			VpcId:            vpc.ID(),
			CidrBlock:        pulumi.String("10.0.2.0/24"),
			AvailabilityZone: pulumi.String("us-east-1b"),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-subnet-2"),
			},
		})
		if err != nil {
			return err
		}

		dbSubnetGroup, err := rds.NewSubnetGroup(ctx, "todo-db-subnet-group", &rds.SubnetGroupArgs{
			SubnetIds: pulumi.StringArray{subnet.ID(), subnet2.ID()},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-db-subnet-group"),
			},
		})
		if err != nil {
			return err
		}

		cluster, err := rds.NewCluster(ctx, "todo-db-cluster", &rds.ClusterArgs{
			Engine:              pulumi.String("aurora-postgresql"),
			EngineMode:          pulumi.String("provisioned"),
			EngineVersion:       pulumi.String("13.6"), // Check available versions
			DatabaseName:        pulumi.String("todoapp"),
			MasterUsername:      pulumi.String("postgres"),
			MasterPassword:      pulumi.String("changeme123"), // TODO: Use secrets
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
			Engine:            cluster.Engine,
			EngineVersion:     cluster.EngineVersion,
		})
		if err != nil {
			return err
		}

		// 4. EC2 Instance with Docker UserData
		userData := `#!/bin/bash
sudo yum update -y
sudo amazon-linux-extras install docker -y
sudo service docker start
sudo usermod -a -G docker ec2-user
sudo chkconfig docker on
sudo curl -L https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m) -o /usr/local/bin/docker-compose
sudo chmod +x /usr/local/bin/docker-compose
`
		// AMI for Amazon Linux 2
		ami, err := ec2.LookupAmi(ctx, &ec2.LookupAmiArgs{
			MostRecent: pulumi.BoolRef(true),
			Owners:     []string{"amazon"},
			Filters: []ec2.GetAmiFilter{
				{
					Name:   "name",
					Values: []string{"amzn2-ami-hvm-*-x86_64-gp2"},
				},
			},
		})
		if err != nil {
			return err
		}

		server, err := ec2.NewInstance(ctx, "todo-server", &ec2.InstanceArgs{
			InstanceType:        pulumi.String("t3.micro"),
			VpcSecurityGroupIds: pulumi.StringArray{webSg.ID()},
			Ami:                 pulumi.String(ami.Id),
			SubnetId:            subnet.ID(),
			UserData:            pulumi.String(userData),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-server"),
			},
		})
		if err != nil {
			return err
		}

		// Outputs
		ctx.Export("publicIp", server.PublicIp)
		ctx.Export("publicHostName", server.PublicDns)
		ctx.Export("dbEndpoint", cluster.Endpoint)

		return nil
	})
}
