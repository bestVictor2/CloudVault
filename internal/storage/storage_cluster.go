package storage

import (
	"CloudVault/config"
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// StorageNode 存储节点
type StorageNode struct {
	Cluster config.MinioClusterConfig
	Client  *minio.Client
	mu      sync.RWMutex
}

// StorageCluster 存储集群
type StorageCluster struct {
	Nodes      []*StorageNode
	currentIdx int // 用于轮询
	mu         sync.Mutex
}

var globalStorageCluster *StorageCluster
var clusterOnce sync.Once

// InitStorageCluster 初始化存储集
func InitStorageCluster() error {
	var initErr error
	clusterOnce.Do(func() {
		cluster := &StorageCluster{
			Nodes: make([]*StorageNode, 0),
		}

		for _, clusterConfig := range config.StorageConfigInstance.MinioClusters {
			node, err := NewStorageNode(clusterConfig)
			if err != nil {
				initErr = fmt.Errorf("failed to create storage node %s: %v", clusterConfig.Name, err)
				return
			}
			cluster.Nodes = append(cluster.Nodes, node)
		}

		globalStorageCluster = cluster
		log.Printf("Storage cluster initialized with %d nodes", len(cluster.Nodes))
	})

	return initErr
}

// NewStorageNode 创建新的存储节点
func NewStorageNode(clusterConfig config.MinioClusterConfig) (*StorageNode, error) {
	client, err := minio.New(
		fmt.Sprintf("%s:%s", clusterConfig.Host, clusterConfig.Port),
		&minio.Options{
			Creds:  credentials.NewStaticV4(clusterConfig.Username, clusterConfig.Password, ""),
			Secure: clusterConfig.UseSSL,
		},
	)
	if err != nil {
		return nil, err
	}

	return &StorageNode{
		Cluster: clusterConfig,
		Client:  client,
	}, nil
}

// GetStorageCluster 获取存储集群实例
func GetStorageCluster() *StorageCluster {
	if globalStorageCluster == nil {
		if err := InitStorageCluster(); err != nil {
			log.Fatalf("Failed to initialize storage cluster: %v", err)
		}
	}
	return globalStorageCluster
}

// SelectNode 选择存储节点
func (sc *StorageCluster) SelectNode() (*StorageNode, error) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	if len(sc.Nodes) == 0 {
		return nil, fmt.Errorf("no available storage nodes")
	}

	switch config.StorageConfigInstance.LoadBalanceStrategy {
	case "round_robin":
		return sc.selectRoundRobin()
	case "least_conn":
		return sc.selectLeastConn()
	case "hash":
		return sc.selectHash()
	default:
		return sc.selectRoundRobin()
	}
}

// selectRoundRobin 轮询选择节点
func (sc *StorageCluster) selectRoundRobin() (*StorageNode, error) {
	// 找到下一个可用的节点
	for i := 0; i < len(sc.Nodes); i++ {
		node := sc.Nodes[sc.currentIdx]
		sc.currentIdx = (sc.currentIdx + 1) % len(sc.Nodes)
		if node.IsAvailable() {
			return node, nil
		}
	}
	return nil, fmt.Errorf("no available storage nodes")
}

// selectLeastConn 选择连接数最少的节点
func (sc *StorageCluster) selectLeastConn() (*StorageNode, error) {
	var selectedNode *StorageNode
	minUsedSize := int64(-1)

	for _, node := range sc.Nodes {
		if !node.IsAvailable() {
			continue
		}
		node.mu.RLock()
		usedSize := node.Cluster.UsedSize
		node.mu.RUnlock()

		if minUsedSize == -1 || usedSize < minUsedSize {
			minUsedSize = usedSize
			selectedNode = node
		}
	}

	if selectedNode == nil {
		return nil, fmt.Errorf("no available storage nodes")
	}

	return selectedNode, nil
}

// selectHash 基于哈希选择节点
func (sc *StorageCluster) selectHash() (*StorageNode, error) {
	// 这里简化实现，实际应该根据文件hash或其他key进行哈希
	// 暂时使用轮询
	return sc.selectRoundRobin()
}

// IsAvailable 检查节点是否可
func (sn *StorageNode) IsAvailable() bool {
	sn.mu.RLock()
	defer sn.mu.RUnlock()
	return sn.Cluster.Available
}

// SetAvailable 设置节点可用
func (sn *StorageNode) SetAvailable(available bool) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.Cluster.Available = available
}

// UpdateUsedSize 更新节点已使用大
func (sn *StorageNode) UpdateUsedSize(delta int64) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.Cluster.UsedSize += delta
}

// GetUsageRate 获取节点使用
func (sn *StorageNode) GetUsageRate() float64 {
	sn.mu.RLock()
	defer sn.mu.RUnlock()
	if sn.Cluster.TotalSize == 0 {
		return 0
	}
	return float64(sn.Cluster.UsedSize) / float64(sn.Cluster.TotalSize) * 100
}

// UploadFileWithReplication 上传文件到多个节点（副本机制）
func (sc *StorageCluster) UploadFileWithReplication(
	ctx context.Context,
	bucketName string,
	objectName string,
	reader ReaderAtSeeker,
	size int64,
	opts minio.PutObjectOptions,
) error {
	// 获取副本
	replicaCount := config.StorageConfigInstance.ReplicaCount
	if replicaCount > len(sc.Nodes) {
		replicaCount = len(sc.Nodes)
	}

	// 上传到第一个节点
	firstNode, err := sc.SelectNode()
	if err != nil {
		return err
	}

	_, err = firstNode.Client.PutObject(ctx, bucketName, objectName, reader, size, opts)
	if err != nil {
		return err
	}

	// 更新节点使用量
	firstNode.UpdateUsedSize(size)

	// 上传到其他节点（副本）
	for i := 1; i < replicaCount; i++ {
		node, err := sc.SelectNode()
		if err != nil {
			log.Printf("Failed to select node for replica %d: %v", i, err)
			continue
		}

		// 重置reader位置
		if _, err := reader.Seek(0, 0); err != nil {
			log.Printf("Failed to seek reader for replica %d: %v", i, err)
			continue
		}

		_, err = node.Client.PutObject(ctx, bucketName, objectName, reader, size, opts)
		if err != nil {
			log.Printf("Failed to upload replica %d: %v", i, err)
			continue
		}

		// 更新节点使用
		node.UpdateUsedSize(size)
	}

	return nil
}

// CheckAndMigrate 检查并迁移文件
func (sc *StorageCluster) CheckAndMigrate(ctx context.Context) error {
	threshold := config.StorageConfigInstance.MigrationThreshold

	for _, node := range sc.Nodes {
		usageRate := node.GetUsageRate()
		if usageRate > float64(threshold) {
			log.Printf("Node %s usage rate %.2f%% exceeds threshold %d%%, starting migration",
				node.Cluster.Name, usageRate, threshold)

			// 查找使用率最低的节点
			targetNode, err := sc.selectLeastConn()
			if err != nil {
				log.Printf("Failed to find target node for migration: %v", err)
				continue
			}

			// 如果目标节点就是当前节点，跳
			if targetNode == node {
				continue
			}

			// 迁移文件
			if err := sc.migrateFiles(ctx, node, targetNode); err != nil {
				log.Printf("Failed to migrate files from %s to %s: %v",
					node.Cluster.Name, targetNode.Cluster.Name, err)
			}
		}
	}

	return nil
}

// migrateFiles 迁移文件
func (sc *StorageCluster) migrateFiles(ctx context.Context, sourceNode, targetNode *StorageNode) error {
	bucketName := config.AppConfig.BucketName

	// 列出源节点的所有对
	objectsCh := sourceNode.Client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{})

	for object := range objectsCh {
		if object.Err != nil {
			log.Printf("Error listing object: %v", object.Err)
			continue
		}

		// 从源节点获取对象
		src, err := sourceNode.Client.GetObject(ctx, bucketName, object.Key, minio.GetObjectOptions{})
		if err != nil {
			log.Printf("Failed to get object %s: %v", object.Key, err)
			continue
		}

		// 上传到目标节
		_, err = targetNode.Client.PutObject(ctx, bucketName, object.Key, src, object.Size, minio.PutObjectOptions{})
		if err != nil {
			log.Printf("Failed to upload object %s to target: %v", object.Key, err)
			src.Close()
			continue
		}
		src.Close()

		// 从源节点删除对象
		err = sourceNode.Client.RemoveObject(ctx, bucketName, object.Key, minio.RemoveObjectOptions{})
		if err != nil {
			log.Printf("Failed to remove object %s from source: %v", object.Key, err)
			continue
		}

		// 更新节点使用
		sourceNode.UpdateUsedSize(-object.Size)
		targetNode.UpdateUsedSize(object.Size)

		log.Printf("Successfully migrated object %s from %s to %s",
			object.Key, sourceNode.Cluster.Name, targetNode.Cluster.Name)
	}

	return nil
}

// StartMigrationMonitor 启动迁移监控
func StartMigrationMonitor(ctx context.Context, interval time.Duration) {
	cluster := GetStorageCluster()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Println("Migration monitor stopped")
				return
			case <-ticker.C:
				if err := cluster.CheckAndMigrate(ctx); err != nil {
					log.Printf("Migration check failed: %v", err)
				}
			}
		}
	}()

	log.Printf("Migration monitor started with interval %v", interval)
}



