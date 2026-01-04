package main

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi-command/sdk/go/command/local"
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
		privateKeyPath := filepath.Join(homeDir, ".ssh", "todo-controlplane-key.pem")
		publicKeyPath := privateKeyPath + ".pub"

		if err := ensureSSHKey(privateKeyPath); err != nil {
			return err
		}

		publicKeyContent, err := os.ReadFile(publicKeyPath)
		if err != nil {
			return err
		}

		// Create AWS Key Pair
		keyPair, err := ec2.NewKeyPair(ctx, "todo-controlplane-key-pair", &ec2.KeyPairArgs{
			PublicKey: pulumi.String(string(publicKeyContent)),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-controlplane-key-pair"),
			},
		})
		if err != nil {
			return err
		}

		// 1. Create a VPC
		vpc, err := ec2.NewVpc(ctx, "todo-controlplane-vpc", &ec2.VpcArgs{
			CidrBlock:          pulumi.String("10.1.0.0/16"), // Different CIDR than app VPC
			EnableDnsHostnames: pulumi.Bool(true),
			EnableDnsSupport:   pulumi.Bool(true),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-controlplane-vpc"),
			},
		})
		if err != nil {
			return err
		}

		// Create an Internet Gateway
		igw, err := ec2.NewInternetGateway(ctx, "todo-controlplane-igw", &ec2.InternetGatewayArgs{
			VpcId: vpc.ID(),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-controlplane-igw"),
			},
		})
		if err != nil {
			return err
		}

		// 2. Subnets
		// Public Subnet
		publicSubnet, err := ec2.NewSubnet(ctx, "todo-controlplane-public-subnet", &ec2.SubnetArgs{
			VpcId:               vpc.ID(),
			CidrBlock:           pulumi.String("10.1.1.0/24"),
			MapPublicIpOnLaunch: pulumi.Bool(true),
			AvailabilityZone:    pulumi.String("eu-west-1a"),
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-controlplane-public-subnet"),
			},
		})
		if err != nil {
			return err
		}

		// Route Table for Public Subnet
		publicRt, err := ec2.NewRouteTable(ctx, "todo-controlplane-public-rt", &ec2.RouteTableArgs{
			VpcId: vpc.ID(),
			Routes: ec2.RouteTableRouteArray{
				&ec2.RouteTableRouteArgs{
					CidrBlock: pulumi.String("0.0.0.0/0"),
					GatewayId: igw.ID(),
				},
			},
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-controlplane-public-rt"),
			},
		})
		if err != nil {
			return err
		}

		// Associate Public Route Table with Public Subnet
		_, err = ec2.NewRouteTableAssociation(ctx, "todo-controlplane-public-rta", &ec2.RouteTableAssociationArgs{
			SubnetId:     publicSubnet.ID(),
			RouteTableId: publicRt.ID(),
		})
		if err != nil {
			return err
		}

		// 3. Security Groups
		sg, err := ec2.NewSecurityGroup(ctx, "todo-controlplane-sg", &ec2.SecurityGroupArgs{
			VpcId:       vpc.ID(),
			Description: pulumi.String("Allow SSH"),
			Ingress: ec2.SecurityGroupIngressArray{
				&ec2.SecurityGroupIngressArgs{
					Protocol:    pulumi.String("tcp"),
					FromPort:    pulumi.Int(22),
					ToPort:      pulumi.Int(22),
					CidrBlocks:  pulumi.StringArray{pulumi.String("0.0.0.0/0")}, // Restrict in production
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

		// 4. EC2 Instance
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

		server, err := ec2.NewInstance(ctx, "todo-controlplane-server", &ec2.InstanceArgs{
			InstanceType:        pulumi.String("t3.micro"),
			VpcSecurityGroupIds: pulumi.StringArray{sg.ID()},
			Ami:                 pulumi.String(ami.Id),
			SubnetId:            publicSubnet.ID(),
			KeyName:             keyPair.KeyName,
			Tags: pulumi.StringMap{
				"Name": pulumi.String("todo-controlplane-server"),
			},
		})
		if err != nil {
			return err
		}

		// Config
		conf := config.New(ctx, "")

		dockerUsername := conf.Require("dockerUsername")
		dockerPassword := conf.RequireSecret("dockerPassword")
		githubToken := conf.RequireSecret("githubToken")

		releaserImage := conf.Get("releaserImage")
		if releaserImage == "" {
			releaserImage = "singaravelan21/todo-releaser"
		}
		releaserVersion := conf.Get("releaserVersion")
		if releaserVersion == "" {
			releaserVersion = "latest"
		}

		// 5. Run Ansible Playbook
		_, err = local.NewCommand(ctx, "run-ansible", &local.CommandArgs{
			Create: pulumi.Sprintf("sleep 60; ANSIBLE_HOST_KEY_CHECKING=False ansible-playbook -vvv -u ec2-user --private-key %s -i '%s,' -e 'docker_username=%s' -e 'docker_password=%s' -e 'github_token=%s' -e 'releaser_image=%s' -e 'releaser_version=%s' ansible/playbook.yml",
				privateKeyPath,
				server.PublicIp,
				dockerUsername,
				dockerPassword,
				githubToken,
				pulumi.String(releaserImage),
				pulumi.String(releaserVersion),
			),
			Triggers: pulumi.Array{
				server.PublicIp,
				dockerPassword,
				githubToken,
			},
		}, pulumi.DependsOn([]pulumi.Resource{server}))
		if err != nil {
			return err
		}

		// Outputs
		ctx.Export("publicIp", server.PublicIp)
		ctx.Export("publicHostName", server.PublicDns)

		return nil
	})
}
