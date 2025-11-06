<script>
  import { onMount } from 'svelte';
  import { SetAPIToken, GetAPIToken, ListProducts, GetProductReleases, GetReleaseFiles, GetReleaseEULA, AcceptEULAAndDownload, GetDownloadLocation, SetDownloadLocation, CancelDownload, GetReleaseDependencySpecifiers, GetReleaseDependencies, GetHTTPProxy, SetHTTPProxy, GetHTTPSProxy, SetHTTPSProxy } from '../wailsjs/go/main/BroadcomService.js';
  import { EventsOn } from '../wailsjs/runtime/runtime.js';
  import { BrowserOpenURL } from '../wailsjs/runtime/runtime.js';
  import tanzuLogo from './assets/images/tile-logo.png';
  import AIModelPackager from './components/AIModelPackager.svelte';

  let apiToken = '';
  let tokenSaved = false;
  let products = [];
  let selectedProduct = null;
  let releases = [];
  let selectedRelease = null;
  let files = [];
  let loading = false;
  let error = '';
  let searchTerm = '';
  let downloads = {};
  let currentView = 'setup'; // setup, products, releases, files, settings, downloads, planner, aimodels
  let showEULAModal = false;
  let currentEULA = null;
  let pendingDownloadFile = null;
  let pendingDownloadContext = null; // Store product/release context for pending download
  let downloadLocation = '';
  let tempDownloadLocation = '';
  let tempApiToken = '';
  let httpProxy = '';
  let httpsProxy = '';
  let tempHttpProxy = '';
  let tempHttpsProxy = '';
  let onlyTanzuPlatform = true; // Default to true - only show Tanzu Platform downloads
  let cancelledDownloads = new Set(); // Track cancelled downloads to ignore late progress events
  let downloadQueue = []; // Queue of pending downloads
  const MAX_PARALLEL_DOWNLOADS = 3;

  // Download Planner state
  let plannerStep = 1; // 1: Select Ops Manager, 2: Select Elastic Runtime, 3: Select TAS Type, 4: Review downloads
  let opsManagerReleases = [];
  let selectedOpsManager = null;
  let elasticRuntimeReleases = [];
  let selectedElasticRuntime = null;
  let selectedTASType = null; // 'full' or 'srt'
  let recommendedProducts = [];
  let plannerLoading = false;
  let plannerError = '';
  let plannerLoadingMessage = '';

  // Toast notification state
  let toastMessage = '';
  let showToast = false;

  // Track clicked buttons for visual feedback
  let clickedButtons = new Set();

  // Bulk EULA acceptance state
  let showBulkEULAModal = false;
  let bulkEULAProgress = '';
  let bulkEULAProducts = [];

  // Reactive download count
  $: activeDownloadCount = Object.values(downloads).filter(d => !d.complete).length + downloadQueue.length;

  onMount(async () => {
    // Load download location
    try {
      downloadLocation = await GetDownloadLocation();
      tempDownloadLocation = downloadLocation;
    } catch (e) {
      console.log('Could not load download location');
    }

    // Load proxy settings
    try {
      httpProxy = await GetHTTPProxy();
      tempHttpProxy = httpProxy;
    } catch (e) {
      console.log('Could not load HTTP proxy');
    }

    try {
      httpsProxy = await GetHTTPSProxy();
      tempHttpsProxy = httpsProxy;
    } catch (e) {
      console.log('Could not load HTTPS proxy');
    }

    // Check if token is already set
    try {
      const token = await GetAPIToken();
      if (token) {
        apiToken = token;
        tokenSaved = true;
        currentView = 'products';
        await loadProducts();
      }
    } catch (e) {
      console.log('No token saved yet');
    }

    // Listen for download progress events
    EventsOn('download-progress', (data) => {
      // Ignore progress events for cancelled downloads
      if (cancelledDownloads.has(data.fileID)) {
        return;
      }

      // Debug: log when we receive totalSize
      if (data.totalSize) {
        console.log('Received file size from OM CLI:', data.totalSize, 'bytes');
      }

      downloads[data.fileID] = {
        ...downloads[data.fileID],
        progress: data.progress,
        downloaded: data.downloaded,
        total: data.total,
        fileSize: data.totalSize || downloads[data.fileID]?.fileSize // Update file size if provided by OM CLI
      };
      downloads = downloads; // Trigger reactivity
    });

    EventsOn('download-complete', (data) => {
      // Remove from cancelled set if it was there
      cancelledDownloads.delete(data.fileID);

      downloads[data.fileID] = {
        ...downloads[data.fileID],
        complete: true,
        path: data.path
      };
      downloads = downloads;

      // Process next item in queue
      processQueue();
    });

    EventsOn('download-cancelled', (data) => {
      // Mark as cancelled to ignore future progress events
      cancelledDownloads.add(data.fileID);

      delete downloads[data.fileID];
      downloads = downloads;

      // Process next item in queue
      processQueue();
    });
  });

  async function saveToken() {
    if (!apiToken.trim()) {
      error = 'Please enter an API token';
      return;
    }
    loading = true;
    error = '';
    try {
      await SetAPIToken(apiToken);
      tokenSaved = true;
      currentView = 'products';
      await loadProducts();
    } catch (e) {
      error = 'Failed to save token: ' + e.toString();
    } finally {
      loading = false;
    }
  }

  async function loadProducts() {
    loading = true;
    error = '';
    try {
      products = await ListProducts();
    } catch (e) {
      error = 'Failed to load products: ' + e.toString();
    } finally {
      loading = false;
    }
  }

  async function selectProduct(product) {
    selectedProduct = product;
    currentView = 'releases';
    loading = true;
    error = '';
    try {
      releases = await GetProductReleases(product.slug);
    } catch (e) {
      error = 'Failed to load releases: ' + e.toString();
    } finally {
      loading = false;
    }
  }

  async function selectRelease(release) {
    selectedRelease = release;
    currentView = 'files';
    loading = true;
    error = '';
    try {
      files = await GetReleaseFiles(selectedProduct.slug, release.id);
    } catch (e) {
      error = 'Failed to load files: ' + e.toString();
    } finally {
      loading = false;
    }
  }

  async function downloadFile(file) {
    // Don't start if already downloading
    if (downloads[file.id] && !downloads[file.id].complete) {
      return;
    }

    // First, fetch the EULA
    error = '';
    try {
      currentEULA = await GetReleaseEULA(selectedProduct.slug, selectedRelease.id);
      if (currentEULA) {
        pendingDownloadFile = file;
        showEULAModal = true;
      } else {
        // No EULA required, proceed with download
        await executeDownload(file);
      }
    } catch (e) {
      error = 'Failed to fetch EULA: ' + e.toString();
    }
  }

  async function acceptEULAAndDownload() {
    showEULAModal = false;
    if (pendingDownloadFile) {
      // Check if we have stored context (from planner) or use current context (from regular flow)
      const product = pendingDownloadContext ? pendingDownloadContext.product : selectedProduct;
      const release = pendingDownloadContext ? pendingDownloadContext.release : selectedRelease;
      const buttonId = pendingDownloadContext ? pendingDownloadContext.buttonId : `${selectedProduct.slug}-${selectedRelease.id}-${pendingDownloadFile.id}`;

      // Set temporary context
      const previousProduct = selectedProduct;
      const previousRelease = selectedRelease;

      selectedProduct = product;
      selectedRelease = release;

      // Add visual feedback to button
      clickedButtons.add(buttonId);
      clickedButtons = clickedButtons; // Trigger reactivity

      // Show toast notification
      showToastNotification(`Download started: ${product.name} v${release.version}`);

      await executeDownload(pendingDownloadFile);

      // Restore previous context
      selectedProduct = previousProduct;
      selectedRelease = previousRelease;

      // Remove visual feedback after a delay
      setTimeout(() => {
        clickedButtons.delete(buttonId);
        clickedButtons = clickedButtons; // Trigger reactivity
      }, 2000);

      pendingDownloadFile = null;
      pendingDownloadContext = null;
      currentEULA = null;
    }
  }

  function cancelEULA() {
    showEULAModal = false;
    pendingDownloadFile = null;
    pendingDownloadContext = null;
    currentEULA = null;
  }

  function getActiveDownloadsCount() {
    return Object.values(downloads).filter(d => !d.complete).length;
  }

  function processQueue() {
    // Process queued downloads if we have capacity
    while (downloadQueue.length > 0 && getActiveDownloadsCount() < MAX_PARALLEL_DOWNLOADS) {
      const queuedItem = downloadQueue.shift();
      downloadQueue = downloadQueue; // Trigger reactivity
      startDownload(queuedItem);
    }
  }

  async function executeDownload(file) {
    // Check if we can start immediately or need to queue
    if (getActiveDownloadsCount() >= MAX_PARALLEL_DOWNLOADS) {
      // Add to queue - capture full context
      downloadQueue = [...downloadQueue, {
        file,
        productName: selectedProduct.name,
        productSlug: selectedProduct.slug,
        version: selectedRelease.version,
        releaseId: selectedRelease.id
      }];
      return;
    }

    // Start download immediately
    await startDownload({
      file,
      productName: selectedProduct.name,
      productSlug: selectedProduct.slug,
      version: selectedRelease.version,
      releaseId: selectedRelease.id
    });
  }

  async function startDownload(item) {
    const { file, productName, productSlug, version, releaseId } = item;

    // Remove from cancelled set when starting a new download
    cancelledDownloads.delete(file.id);

    // Pass only the directory - OM CLI will use its own filename and can resume
    const savePath = downloadLocation;
    downloads[file.id] = {
      fileName: file.name,
      productName: productName,
      version: version,
      fileSize: null, // File size not available from API
      progress: 0,
      complete: false,
      queued: false
    };
    downloads = downloads;

    try {
      // Use the captured context from the queued item
      await AcceptEULAAndDownload(productSlug, releaseId, file.id, savePath);
    } catch (e) {
      error = 'Failed to download file: ' + e.toString();
      delete downloads[file.id];
      downloads = downloads;
      // Process next in queue
      processQueue();
    }
  }

  async function cancelDownload(fileId) {
    try {
      await CancelDownload(fileId);
    } catch (e) {
      error = 'Failed to cancel download: ' + e.toString();
    }
  }

  function backToProducts() {
    currentView = 'products';
    selectedProduct = null;
    selectedRelease = null;
    files = [];
    releases = [];
  }

  function backToReleases() {
    currentView = 'releases';
    selectedRelease = null;
    files = [];
  }

  function changeToken() {
    tokenSaved = false;
    currentView = 'setup';
    products = [];
    selectedProduct = null;
    releases = [];
    selectedRelease = null;
    files = [];
  }

  function openGitHub() {
    BrowserOpenURL('https://github.com/odedia/tile-downloader');
  }

  function openSettings() {
    tempDownloadLocation = downloadLocation;
    tempApiToken = apiToken;
    tempHttpProxy = httpProxy;
    tempHttpsProxy = httpsProxy;
    currentView = 'settings';
  }

  async function saveSettings() {
    loading = true;
    error = '';
    try {
      // Save API token if it changed
      if (tempApiToken !== apiToken) {
        await SetAPIToken(tempApiToken);
        apiToken = tempApiToken;
        tokenSaved = true;
      }

      await SetDownloadLocation(tempDownloadLocation);
      downloadLocation = tempDownloadLocation;

      // Save proxy settings
      await SetHTTPProxy(tempHttpProxy);
      httpProxy = tempHttpProxy;

      await SetHTTPSProxy(tempHttpsProxy);
      httpsProxy = tempHttpsProxy;

      currentView = 'products';
    } catch (e) {
      error = 'Failed to save settings: ' + e.toString();
    } finally {
      loading = false;
    }
  }

  function cancelSettings() {
    tempDownloadLocation = downloadLocation;
    tempApiToken = apiToken;
    tempHttpProxy = httpProxy;
    tempHttpsProxy = httpsProxy;
    currentView = 'products';
  }

  // Download Planner functions
  async function openDownloadPlanner() {
    currentView = 'planner';
    plannerStep = 1;
    plannerError = '';
    selectedOpsManager = null;
    selectedElasticRuntime = null;
    recommendedProducts = [];

    // Load Ops Manager releases
    plannerLoading = true;
    try {
      const releases = await GetProductReleases('ops-manager');
      opsManagerReleases = releases.sort((a, b) => b.version.localeCompare(a.version));
    } catch (e) {
      plannerError = 'Failed to load Ops Manager releases: ' + e.toString();
    } finally {
      plannerLoading = false;
    }
  }

  async function selectOpsManagerVersion(release) {
    selectedOpsManager = release;
    plannerStep = 2;
    plannerLoading = true;
    plannerError = '';
    plannerLoadingMessage = 'Finding compatible TAS versions...';

    try {
      // Get all Elastic Runtime (TAS) releases
      const allReleases = await GetProductReleases('elastic-runtime');

      // Sort by version descending
      const sortedReleases = allReleases.sort((a, b) => compareVersions(b.version, a.version));

      // Check recent releases (last 50) to find compatible ones
      const recentReleases = sortedReleases.slice(0, 50);
      const compatibleReleases = [];

      for (const tasRelease of recentReleases) {
        try {
          // Get dependency specifiers for this TAS release
          const tasDependencySpecifiers = await GetReleaseDependencySpecifiers('elastic-runtime', tasRelease.id);

          // Look for Ops Manager dependency
          const opsManagerDeps = tasDependencySpecifiers.filter(d =>
            d.product.slug === 'ops-manager' ||
            d.product.name.toLowerCase().includes('ops manager')
          );

          if (opsManagerDeps.length > 0) {
            // Check if our selected Ops Manager version matches ANY of the Ops Manager dependency specifiers
            const isCompatible = opsManagerDeps.some(dep => {
              const specs = dep.specifier.split(',').map(s => s.trim());
              return specs.some(spec => versionMatchesSpecifier(release.version, spec));
            });

            if (isCompatible) {
              compatibleReleases.push(tasRelease);
            }
          }
        } catch (e) {
          console.log(`Error checking TAS v${tasRelease.version}:`, e);
        }
      }

      elasticRuntimeReleases = compatibleReleases;
    } catch (e) {
      plannerError = 'Failed to load Elastic Runtime releases: ' + e.toString();
    } finally {
      plannerLoading = false;
      plannerLoadingMessage = '';
    }
  }

  function selectElasticRuntimeVersion(release) {
    selectedElasticRuntime = release;
    plannerStep = 3; // Go to TAS type selection
    plannerError = '';
  }

  async function selectTASType(tasType) {
    selectedTASType = tasType;
    plannerStep = 4; // Go to review downloads
    plannerLoading = true;
    plannerError = '';

    try {
      recommendedProducts = [];

      // First, add Ops Manager and TAS to the download list
      plannerLoadingMessage = 'Adding Ops Manager...';
      const opsManagerFiles = await GetReleaseFiles('ops-manager', selectedOpsManager.id);
      const opsManagerMainFiles = [];
      opsManagerFiles.forEach(file => {
        const fileName = file.name.toLowerCase();
        if (fileName.includes('vsphere') || fileName.includes('vmware')) {
          opsManagerMainFiles.push(file);
        }
      });

      recommendedProducts.push({
        productName: 'Ops Manager',
        productSlug: 'ops-manager',
        version: selectedOpsManager.version,
        releaseId: selectedOpsManager.id,
        files: opsManagerMainFiles.length > 0 ? opsManagerMainFiles : opsManagerFiles,
        priority: 0,
        actualSlug: 'ops-manager'
      });

      plannerLoadingMessage = 'Adding Tanzu Application Service...';
      const tasFiles = await GetReleaseFiles('elastic-runtime', selectedElasticRuntime.id);

      // Filter based on selected TAS type - only include the main .pivotal tile and CF CLI
      let tasFilesToDownload = [];
      if (selectedTASType === 'srt') {
        // Small Footprint Runtime - only include .pivotal files with "Small Footprint" in the name
        const srtTile = tasFiles.filter(f => {
          const name = f.name.toLowerCase();
          const awsKey = (f.aws_object_key || '').toLowerCase();
          const isPivotal = awsKey.endsWith('.pivotal');
          const isSmallFootprint = (name.includes('small') && name.includes('footprint')) ||
                                   (name.includes('small') && name.includes('tpcf'));
          return isPivotal && isSmallFootprint;
        });
        tasFilesToDownload.push(...srtTile);
      } else {
        // Full TAS - only include .pivotal files, excluding Small Footprint, OSL, ODP, and plugins
        const fullTile = tasFiles.filter(f => {
          const name = f.name.toLowerCase();
          const awsKey = (f.aws_object_key || '').toLowerCase();
          const isPivotal = awsKey.endsWith('.pivotal');
          const isMainTile = name.includes('tanzu platform for cloud foundry') ||
                            name.includes('tanzu application service');
          const isNotSmall = !name.includes('small') && !name.includes('footprint');
          const isNotOSL = !name.includes('osl');
          const isNotODP = !name.includes('odp');
          const isNotPlugin = !name.includes('plugin');
          return isPivotal && isMainTile && isNotSmall && isNotOSL && isNotODP && isNotPlugin;
        });
        tasFilesToDownload.push(...fullTile);
      }

      // Add CF CLI for both types - only CF CLI files (exclude OSL, plugins, and non-CLI files)
      const cfCLI = tasFiles.filter(f => {
        const name = f.name.toLowerCase();
        const awsKey = (f.aws_object_key || '').toLowerCase();
        const isCFCLI = name.includes('cf') && name.includes('cli');
        const isNotOSL = !name.includes('osl');
        const isNotPlugin = !name.includes('plugin');
        // Additional check: CF CLI files typically have specific extensions or patterns
        const hasValidPattern = awsKey.includes('cf-cli') || awsKey.includes('cf_cli');
        return isCFCLI && isNotOSL && isNotPlugin && hasValidPattern;
      });
      tasFilesToDownload.push(...cfCLI);

      const tasDisplayName = selectedTASType === 'srt'
        ? 'Tanzu Application Service (Small Footprint)'
        : 'Tanzu Application Service (Full)';

      recommendedProducts.push({
        productName: tasDisplayName,
        productSlug: 'elastic-runtime',
        version: selectedElasticRuntime.version,
        releaseId: selectedElasticRuntime.id,
        files: tasFilesToDownload,
        priority: 0.5,
        actualSlug: 'elastic-runtime'
      });

      // Product slugs we want to recommend
      const targetProducts = [
        { slug: 'vmware-postgres-for-tas', name: 'Postgres', priority: 1 },
        { slug: 'genai-for-tas', name: 'AI Services (GenAI)', priority: 2 },
        { slug: 'p-rabbitmq', name: 'RabbitMQ', priority: 3 },
        { slug: 'pivotal-mysql', name: 'MySQL', priority: 4 },
        { slug: 'p-redis', name: 'Valkey', priority: 5 },
        { slug: 'tanzu-gemfire-for-vms', name: 'Gemfire', priority: 6 },
        { slug: 'apm', name: 'App Metrics', priority: 7 },
        { slug: 'p-metric-store', name: 'Metric Store', priority: 8 },
        { slug: 'pas-windows', name: 'Windows Add On', priority: 9 },
        { slug: 'pivotal_single_sign-on_service', name: 'Single Sign-On', priority: 10 },
        { slug: 'p-spring-cloud-services', name: 'Spring Cloud Services', priority: 11 },
        { slug: 'spring-cloud-gateway', name: 'Spring Cloud Gateway', priority: 12 },
        { slug: 'dataflow', name: 'Tanzu Data Flow', priority: 13 },
        { slug: 'stemcells-ubuntu-jammy', name: 'Stemcells (Ubuntu Jammy)', priority: 14 },
        { slug: 'credhub-service-broker', name: 'Credhub', priority: 15 },
        { slug: 'p-scheduler', name: 'Scheduler', priority: 16 },
      ];

      // For each tile, check which of its releases are compatible with our selected TAS version
      for (const target of targetProducts) {
        try {
          plannerLoadingMessage = `Checking ${target.name}...`;
          console.log(`\nChecking ${target.name} (${target.slug})...`);

          // Get all releases for this tile
          const tileReleases = await GetProductReleases(target.slug);
          console.log(`Found ${tileReleases.length} releases for ${target.name}`);

          // Sort releases by version descending using semantic version comparison
          const sortedReleases = tileReleases.sort((a, b) => compareVersions(b.version, a.version));

          // Check if this is a stemcell product
          const isStemcellProduct = target.slug.toLowerCase().includes('stemcell');

          let compatibleRelease = null;

          if (isStemcellProduct) {
            // For stemcells, just use the latest version (they don't have TAS dependencies)
            compatibleRelease = sortedReleases[0];
            console.log(`Using latest stemcell version: ${compatibleRelease.version}`);
          } else {
            // Only check the most recent 20 releases to avoid excessive API calls
            const recentReleases = sortedReleases.slice(0, 20);
            console.log(`Checking ${recentReleases.length} most recent releases`);

            // Check each release (starting with latest) to find one compatible with our TAS version
            for (const tileRelease of recentReleases) {
              try {
                // Get this tile release's dependencies - try both endpoints
                const tileDependencySpecifiers = await GetReleaseDependencySpecifiers(target.slug, tileRelease.id);
                const tileDependencies = await GetReleaseDependencies(target.slug, tileRelease.id);

                console.log(`Full dependencies for ${target.name} v${tileRelease.version}:`, {
                  specifiers: tileDependencySpecifiers,
                  dependencies: tileDependencies
                });

                // Look for ALL elastic-runtime / TAS dependencies (there can be multiple!)
                const tasDependencies = tileDependencySpecifiers.filter(d =>
                  d.product.slug === 'elastic-runtime' ||
                  d.product.slug === 'cf' ||
                  d.product.name.toLowerCase().includes('elastic runtime') ||
                  d.product.name.toLowerCase().includes('tanzu application service')
                );

                if (tasDependencies.length > 0) {
                  const allSpecifiers = tasDependencies.map(d => d.specifier).join(', ');
                  console.log(`${target.name} v${tileRelease.version} requires TAS: ${allSpecifiers}`);

                  // Check if our selected TAS version matches ANY of the TAS dependency specifiers
                  const isCompatible = tasDependencies.some(dep => {
                    // Each specifier might also be comma-separated (though usually not)
                    const specs = dep.specifier.split(',').map(s => s.trim());
                    return specs.some(spec => versionMatchesSpecifier(selectedElasticRuntime.version, spec));
                  });

                  if (isCompatible) {
                    console.log(`✓ ${target.name} v${tileRelease.version} IS compatible with TAS ${selectedElasticRuntime.version}`);
                    compatibleRelease = tileRelease;
                    break; // Found the latest compatible version
                  } else {
                    console.log(`✗ ${target.name} v${tileRelease.version} not compatible with TAS ${selectedElasticRuntime.version}`);
                  }
                } else {
                  console.log(`${target.name} v${tileRelease.version} has no TAS dependency specified`);
                }
              } catch (e) {
                console.log(`Error checking ${target.name} v${tileRelease.version}:`, e);
              }
            }
          }

          if (compatibleRelease) {
            // Get files for this release
            const allFiles = await GetReleaseFiles(target.slug, compatibleRelease.id);

            // Use the same categorization logic as the main download screen
            const isStemcellProduct = target.slug.toLowerCase().includes('stemcell');
            const isOpsManagerProduct = target.slug.toLowerCase().includes('ops-manager');

            let mainFiles = [];

            allFiles.forEach(file => {
              const fileName = file.name.toLowerCase();
              const awsObjectKey = file.aws_object_key ? file.aws_object_key.toLowerCase() : '';
              const fileType = file.file_type ? file.file_type.toLowerCase() : '';

              let isMainFile = false;

              if (isStemcellProduct) {
                // Only vSphere stemcells
                if (fileName.includes('vsphere')) {
                  isMainFile = true;
                }
              } else if (isOpsManagerProduct) {
                // Only vSphere Ops Manager
                if (fileName.includes('vsphere') || fileName.includes('vmware')) {
                  isMainFile = true;
                }
              } else {
                // For tiles: .pivotal files
                const isPivotalFile = fileName.endsWith('.pivotal') ||
                                     awsObjectKey.endsWith('.pivotal') ||
                                     (fileType && fileType.includes('pivotal'));
                if (isPivotalFile) {
                  isMainFile = true;
                }
              }

              if (isMainFile) {
                mainFiles.push(file);
              }
            });

            // If no main files found, log a warning
            if (mainFiles.length === 0) {
              console.log(`Warning: No main files found for ${target.name} v${compatibleRelease.version}`);
              console.log('Available files:', allFiles.map(f => f.name));
            }

            recommendedProducts.push({
              productName: target.name,
              productSlug: target.slug, // Use the slug we searched with
              version: compatibleRelease.version,
              releaseId: compatibleRelease.id,
              files: mainFiles.length > 0 ? mainFiles : allFiles, // Fallback to all files if none found
              priority: target.priority,
              actualSlug: target.slug // Store for download operations
            });
          } else {
            console.log(`No compatible version of ${target.name} found for TAS ${release.version}`);
          }
        } catch (e) {
          console.log(`Could not load ${target.name}:`, e);
        }
      }

      console.log('\nRecommended products:', recommendedProducts);

      recommendedProducts.sort((a, b) => a.priority - b.priority);
    } catch (e) {
      plannerError = 'Failed to load product recommendations: ' + e.toString();
    } finally {
      plannerLoading = false;
      plannerLoadingMessage = '';
    }
  }

  function compareVersions(v1, v2) {
    // Compare two semantic versions
    // Returns: 1 if v1 > v2, -1 if v1 < v2, 0 if equal

    // Clean versions (remove build metadata)
    const clean1 = v1.split('+')[0].split('-')[0];
    const clean2 = v2.split('+')[0].split('-')[0];

    const parts1 = clean1.split('.').map(p => parseInt(p) || 0);
    const parts2 = clean2.split('.').map(p => parseInt(p) || 0);

    // Compare each part
    for (let i = 0; i < Math.max(parts1.length, parts2.length); i++) {
      const p1 = parts1[i] || 0;
      const p2 = parts2[i] || 0;

      if (p1 > p2) return 1;
      if (p1 < p2) return -1;
    }

    return 0;
  }

  function versionMatchesSpecifier(version, specifier) {
    // Strip any build metadata from version (like +LTS-T)
    const cleanVersion = version.split('+')[0].split('-')[0];

    console.log(`Checking if "${cleanVersion}" (from "${version}") matches "${specifier}"`);

    // Check for version range (e.g., "2.11.16 - 2.11.58")
    if (specifier.includes(' - ')) {
      const [minVer, maxVer] = specifier.split(' - ').map(v => v.trim());
      const matches = cleanVersion >= minVer && cleanVersion <= maxVer;
      console.log(`Range ${minVer} - ${maxVer}: ${minVer} <= ${cleanVersion} <= ${maxVer}? ${matches}`);
      return matches;
    }

    // Check if a version satisfies a specifier
    if (specifier.startsWith('~>')) {
      const baseVersion = specifier.replace('~>', '').trim();
      const baseParts = baseVersion.split('.');
      const versionParts = cleanVersion.split('.');

      const baseMajor = parseInt(baseParts[0]);
      const baseMinor = baseParts[1] ? parseInt(baseParts[1]) : 0;
      const vMajor = parseInt(versionParts[0]);
      const vMinor = versionParts[1] ? parseInt(versionParts[1]) : 0;

      if (baseParts.length === 1) {
        // ~> 3 means >= 3.0 and < 4.0
        const matches = vMajor === baseMajor;
        console.log(`~> ${baseVersion}: major ${vMajor} === ${baseMajor}? ${matches}`);
        return matches;
      } else {
        // ~> 3.0 means >= 3.0 and < 3.1
        const matches = vMajor === baseMajor && vMinor === baseMinor;
        console.log(`~> ${baseVersion}: ${vMajor}.${vMinor} === ${baseMajor}.${baseMinor}? ${matches}`);
        return matches;
      }
    } else if (specifier.startsWith('>=')) {
      const minVersion = specifier.replace('>=', '').trim();
      const matches = cleanVersion >= minVersion;
      console.log(`>= ${minVersion}: ${cleanVersion} >= ${minVersion}? ${matches}`);
      return matches;
    } else if (specifier.includes('*')) {
      // Convert version pattern like "6.0.*" to regex
      // Replace dots with escaped dots, then replace * with digit pattern
      const escapedPattern = specifier.replace(/\./g, '\\.').replace(/\*/g, '\\d+');
      const regex = new RegExp(`^${escapedPattern}$`);
      const matches = regex.test(cleanVersion);
      console.log(`Pattern ${specifier}: ${cleanVersion} matches /${escapedPattern}/? ${matches}`);
      return matches;
    } else {
      // Exact match
      const matches = cleanVersion === specifier;
      console.log(`Exact: ${cleanVersion} === ${specifier}? ${matches}`);
      return matches;
    }
  }

  function filterReleasesBySpecifier(releases, specifier) {
    // Parse specifier (e.g., "~> 3.0", ">=2.11", "2.10.*")
    // For simplicity, we'll implement basic version matching

    if (specifier.startsWith('~>')) {
      // Pessimistic version constraint: ~> 3.0 means >= 3.0 and < 4.0
      const baseVersion = specifier.replace('~>', '').trim();
      const parts = baseVersion.split('.');
      const major = parseInt(parts[0]);
      const minor = parts[1] ? parseInt(parts[1]) : 0;

      return releases.filter(r => {
        const vParts = r.version.split('.');
        const vMajor = parseInt(vParts[0]);
        const vMinor = vParts[1] ? parseInt(vParts[1]) : 0;

        if (parts.length === 1) {
          // ~> 3 means >= 3.0 and < 4.0
          return vMajor === major;
        } else {
          // ~> 3.0 means >= 3.0 and < 3.1
          return vMajor === major && vMinor === minor;
        }
      }).sort((a, b) => b.version.localeCompare(a.version));
    } else if (specifier.startsWith('>=')) {
      const minVersion = specifier.replace('>=', '').trim();
      return releases.filter(r => r.version >= minVersion).sort((a, b) => b.version.localeCompare(a.version));
    } else if (specifier.includes('*')) {
      // Wildcard: 2.10.*
      const pattern = specifier.replace(/\*/g, '.*');
      const regex = new RegExp(`^${pattern}$`);
      return releases.filter(r => regex.test(r.version)).sort((a, b) => b.version.localeCompare(a.version));
    } else {
      // Exact match
      return releases.filter(r => r.version === specifier);
    }
  }

  function backToPlannerStep(step) {
    plannerStep = step;
    plannerError = '';
  }

  function showToastNotification(message) {
    toastMessage = message;
    showToast = true;
    setTimeout(() => {
      showToast = false;
    }, 3000);
  }

  async function downloadPlannerProduct(product, file) {
    // Create a temporary product and release context for the download
    const tempProduct = { name: product.productName, slug: product.productSlug };
    const tempRelease = { version: product.version, id: product.releaseId };

    // Fetch EULA first
    error = '';
    try {
      currentEULA = await GetReleaseEULA(product.productSlug, product.releaseId);
      if (currentEULA) {
        // Show EULA modal - store context for later use
        pendingDownloadFile = file;
        pendingDownloadContext = {
          product: tempProduct,
          release: tempRelease,
          buttonId: `${product.productSlug}-${product.releaseId}-${file.id}`
        };
        showEULAModal = true;
      } else {
        // No EULA required, proceed with download
        // Set temporary context
        const previousProduct = selectedProduct;
        const previousRelease = selectedRelease;

        selectedProduct = tempProduct;
        selectedRelease = tempRelease;

        // Add visual feedback to button
        const buttonId = `${product.productSlug}-${product.releaseId}-${file.id}`;
        clickedButtons.add(buttonId);
        clickedButtons = clickedButtons; // Trigger reactivity

        // Show toast notification immediately
        showToastNotification(`Download started: ${product.productName} v${product.version}`);

        await executeDownload(file);

        // Restore previous context
        selectedProduct = previousProduct;
        selectedRelease = previousRelease;

        // Remove visual feedback after a delay
        setTimeout(() => {
          clickedButtons.delete(buttonId);
          clickedButtons = clickedButtons; // Trigger reactivity
        }, 2000);
      }
    } catch (e) {
      error = 'Failed to fetch EULA: ' + e.toString();
    }
  }

  function downloadAllPlannerProducts() {
    // Show bulk EULA acceptance modal (user must approve)
    bulkEULAProducts = recommendedProducts.filter(p => p.files && p.files.length > 0);
    bulkEULAProgress = `Ready to accept EULAs for ${bulkEULAProducts.length} products`;
    showBulkEULAModal = true;
  }

  async function acceptAllEULAsAndDownload() {
    // User approved - now process all EULAs and queue downloads
    let acceptedCount = 0;

    // First, accept all EULAs without waiting for downloads
    for (const product of bulkEULAProducts) {
      if (!product.files || product.files.length === 0) continue;

      const file = product.files[0];
      bulkEULAProgress = `Accepting EULA ${acceptedCount + 1}/${bulkEULAProducts.length}: ${product.productName}`;

      try {
        // Check for EULA and accept it (this is fast)
        const eula = await GetReleaseEULA(product.productSlug, product.releaseId);
        if (eula) {
          console.log(`Auto-accepting EULA for ${product.productName}`);
        }

        acceptedCount++;

      } catch (e) {
        console.error(`Failed to accept EULA for ${product.productName}:`, e);
      }
    }

    // Now queue all downloads (don't wait for them to complete)
    bulkEULAProgress = `Queueing ${bulkEULAProducts.length} downloads...`;

    for (const product of bulkEULAProducts) {
      if (!product.files || product.files.length === 0) continue;

      const file = product.files[0];

      // Wrap in an IIFE to capture the product context
      (async (prod, f) => {
        try {
          // Create context for this specific download
          const tempProduct = { name: prod.productName, slug: prod.productSlug };
          const tempRelease = { version: prod.version, id: prod.releaseId };

          // Store previous context
          const prevProduct = selectedProduct;
          const prevRelease = selectedRelease;

          // Set context for this download
          selectedProduct = tempProduct;
          selectedRelease = tempRelease;

          // Add visual feedback to button
          const buttonId = `${prod.productSlug}-${prod.releaseId}-${f.id}`;
          clickedButtons.add(buttonId);
          clickedButtons = clickedButtons;

          // Execute the download
          await executeDownload(f);

          // Restore previous context
          selectedProduct = prevProduct;
          selectedRelease = prevRelease;

          // Remove visual feedback after a delay
          setTimeout(() => {
            clickedButtons.delete(buttonId);
            clickedButtons = clickedButtons;
          }, 2000);

        } catch (e) {
          console.error(`Failed to download ${prod.productName}:`, e);
        }
      })(product, file);
    }

    // All done
    bulkEULAProgress = `All EULAs accepted! ${bulkEULAProducts.length} downloads queued`;

    // Wait a moment then close modal and switch view
    setTimeout(() => {
      showBulkEULAModal = false;
      bulkEULAProgress = '';
      showToastNotification(`Queued ${bulkEULAProducts.length} products for download`);
      currentView = 'downloads';
    }, 1000);
  }

  function cancelBulkEULA() {
    showBulkEULAModal = false;
    bulkEULAProgress = '';
    bulkEULAProducts = [];
  }

  // Filter products based on search term and Tanzu Platform setting
  $: filteredProducts = (() => {
    let filtered = products;

    // Apply Tanzu Platform filter if enabled
    if (onlyTanzuPlatform) {
      // Find products with "on Tanzu Platform" suffix
      const tanzuPlatformProducts = new Set(
        filtered
          .filter(p => p.name.includes('on Tanzu Platform'))
          .map(p => p.name.replace(/\s+on Tanzu Platform$/i, '').trim())
      );

      filtered = filtered.filter(p => {
        const productName = p.name;

        // Exclude products with "Kubernetes" in the name
        if (productName.toLowerCase().includes('kubernetes')) {
          return false;
        }

        // Exclude products with "Greenplum" in the name
        if (productName.toLowerCase().includes('greenplum')) {
          return false;
        }

        // Get the base product name (without suffix)
        const baseName = productName.replace(/\s+on (Tanzu Platform|Kubernetes)$/i, '').trim();

        // If this is a "on Tanzu Platform" product, include it
        if (productName.includes('on Tanzu Platform')) {
          return true;
        }

        // If there's a "on Tanzu Platform" version of this product, exclude this one
        if (tanzuPlatformProducts.has(baseName)) {
          return false;
        }

        // Otherwise include the product
        return true;
      });
    }

    // Apply search filter
    filtered = filtered.filter(p =>
      p.name.toLowerCase().includes(searchTerm.toLowerCase()) ||
      p.slug.toLowerCase().includes(searchTerm.toLowerCase())
    );

    // Sort products with priority
    filtered.sort((a, b) => {
      // Define priority order by slug
      const priorityOrder = {
        'ops-manager': 1,
        'elastic-runtime': 2,
        'vmware-postgres-for-tas': 3,
        'genai-for-tas': 4,
        'p-rabbitmq': 5,
        'pivotal-mysql': 6,
        'p-redis': 7,
        'tanzu-gemfire-for-vms': 8,
        'apm': 9,
        'p-metric-store': 10,
        'pas-windows': 11,
        'pivotal_single_sign-on_service': 12,
        'p-spring-cloud-services': 13,
        'spring-cloud-gateway': 14,
        'dataflow': 15,
        'stemcells-ubuntu-jammy': 16,
        'credhub-service-broker': 17,
        'p-scheduler': 18,
      };

      const aPriority = priorityOrder[a.slug] || 999;
      const bPriority = priorityOrder[b.slug] || 999;

      // Sort by priority first
      if (aPriority !== bPriority) {
        return aPriority - bPriority;
      }

      // If same priority (or both unprioritized), sort alphabetically
      return a.name.localeCompare(b.name);
    });

    return filtered;
  })();

  // Generate OM CLI command for a file
  function generateOMCommand(file) {
    const isStemcell = selectedProduct.slug.toLowerCase().includes('stemcell');
    const isOpsManager = selectedProduct.slug.toLowerCase().includes('ops-manager');
    const fileName = file.name;

    if (isStemcell) {
      // Extract IaaS from filename
      let stemcellIaas = "vsphere"; // Default
      const lowerName = fileName.toLowerCase();
      if (lowerName.includes("vsphere")) {
        stemcellIaas = "vsphere";
      } else if (lowerName.includes("aws")) {
        stemcellIaas = "aws";
      } else if (lowerName.includes("azure")) {
        stemcellIaas = "azure";
      } else if (lowerName.includes("google")) {
        stemcellIaas = "google";
      }

      return `om download-product \\
  -t "\${API_TOKEN}" \\
  -p ${selectedProduct.slug} \\
  --product-version ${selectedRelease.version} \\
  --stemcell-iaas ${stemcellIaas} \\
  -o "\${OUTPUT_DIRECTORY}"`;
    } else if (isOpsManager) {
      // Extract IaaS from filename for Ops Manager
      let opsManagerIaas = "vsphere"; // Default
      const lowerName = fileName.toLowerCase();
      if (lowerName.includes("vsphere")) {
        opsManagerIaas = "vsphere";
      } else if (lowerName.includes("aws")) {
        opsManagerIaas = "aws";
      } else if (lowerName.includes("azure")) {
        opsManagerIaas = "azure";
      } else if (lowerName.includes("gcp") || lowerName.includes("google")) {
        opsManagerIaas = "gcp";
      } else if (lowerName.includes("openstack")) {
        opsManagerIaas = "openstack";
      }

      return `om download-product \\
  -t "\${API_TOKEN}" \\
  -p ${selectedProduct.slug} \\
  --product-version ${selectedRelease.version} \\
  -f "*${opsManagerIaas}*" \\
  -o "\${OUTPUT_DIRECTORY}"`;
    } else {
      // For tiles, use glob pattern if fileName doesn't end with .pivotal
      let fileGlob = fileName;
      if (!fileName.includes("*") && !fileName.toLowerCase().endsWith(".pivotal")) {
        fileGlob = "*.pivotal";
      }

      return `om download-product \\
  -t "\${API_TOKEN}" \\
  -p ${selectedProduct.slug} \\
  --product-version ${selectedRelease.version} \\
  -f "${fileGlob}" \\
  -o "\${OUTPUT_DIRECTORY}"`;
    }
  }

  // Copy OM CLI command to clipboard
  async function copyOMCommand(file) {
    const command = generateOMCommand(file);
    try {
      await navigator.clipboard.writeText(command);
      alert('OM CLI command copied to clipboard!');
    } catch (err) {
      console.error('Failed to copy command:', err);
      alert('Failed to copy command to clipboard');
    }
  }

  // Categorize and prioritize files based on product context
  function categorizeFiles(fileList) {
    const mainFiles = [];
    const otherFiles = [];

    // Determine product context
    const productName = selectedProduct ? selectedProduct.name.toLowerCase() : '';
    const productSlug = selectedProduct ? selectedProduct.slug.toLowerCase() : '';

    // Check both name and slug for stemcell detection
    const isStemcellProduct = productName.includes('stemcell') || productSlug.includes('stemcell');
    const isOpsManagerProduct = productName.includes('foundation core') || productSlug.includes('ops-manager');

    console.log('Product Name:', productName);
    console.log('Product Slug:', productSlug);
    console.log('Is Stemcell Product:', isStemcellProduct);

    fileList.forEach(file => {
      console.log('Full file object:', file);
      const fileName = file.name.toLowerCase();
      const fileType = file.file_type ? file.file_type.toLowerCase() : '';
      const awsObjectKey = file.aws_object_key ? file.aws_object_key.toLowerCase() : '';

      console.log('Processing file:', file.name);
      console.log('  - fileName:', fileName);
      console.log('  - fileType:', fileType);
      console.log('  - awsObjectKey:', awsObjectKey);
      console.log('  - Ends with .pivotal?', fileName.endsWith('.pivotal'));
      console.log('  - aws_object_key ends with .pivotal?', awsObjectKey.endsWith('.pivotal'));

      let isMainFile = false;

      // For Stemcell products: Only vSphere stemcells in main, rest in other
      if (isStemcellProduct) {
        // Only vSphere stemcells are main downloads
        if (fileName.includes('vsphere')) {
          isMainFile = true;
        }
      }
      // For Ops Manager (Foundation Core): Only vSphere in main, rest in other
      else if (isOpsManagerProduct) {
        // Only vSphere Ops Manager goes to main section
        if (fileName.includes('vsphere') || fileName.includes('vmware')) {
          isMainFile = true;
        }
        // Other Ops Manager variants (AWS, Azure, GCP, etc.) go to "Other Downloads"
      }
      // For Tanzu Tile products: .pivotal files are the main downloads
      else {
        // Check fileName, file_type, and aws_object_key for pivotal files
        const isPivotalFile = fileName.endsWith('.pivotal') ||
                             awsObjectKey.endsWith('.pivotal') ||
                             (fileType && fileType.toLowerCase().includes('pivotal'));

        if (isPivotalFile) {
          isMainFile = true;
        }
      }

      console.log('File:', fileName, 'Is Main:', isMainFile);

      if (isMainFile) {
        mainFiles.push(file);
      } else {
        otherFiles.push(file);
      }
    });

    // Sort main files: prioritize vSphere variants
    mainFiles.sort((a, b) => {
      const aName = a.name.toLowerCase();
      const bName = b.name.toLowerCase();

      // Prioritize vSphere
      const aIsVSphere = aName.includes('vsphere') || aName.includes('vmware');
      const bIsVSphere = bName.includes('vsphere') || bName.includes('vmware');

      if (aIsVSphere && !bIsVSphere) return -1;
      if (!aIsVSphere && bIsVSphere) return 1;

      return 0;
    });

    return { mainFiles, otherFiles };
  }

  $: categorizedFiles = categorizeFiles(files);
</script>

<main>
  <header>
    <div class="header-title">
      <img src={tanzuLogo} alt="VMware Tanzu" class="tanzu-logo" />
      <h1>Tile Downloader</h1>
    </div>
    {#if tokenSaved}
      <div class="header-buttons">
        <button class="planner-btn" on:click={openDownloadPlanner}>📋 Download Planner</button>
        <button class="aimodels-btn" on:click={() => currentView = 'aimodels'}>🤖 AI Model Packager</button>
        <button class="downloads-btn" on:click={() => currentView = 'downloads'}>
          📥 Active Downloads {activeDownloadCount > 0 ? `(${activeDownloadCount})` : ''}
        </button>
        <button class="settings-btn" on:click={openSettings}>⚙️ Settings</button>
      </div>
    {/if}
  </header>

  {#if error}
    <div class="error">
      {error}
      <button class="error-dismiss" on:click={() => error = ''}>×</button>
    </div>
  {/if}

  {#if showEULAModal && currentEULA}
    <div class="modal-overlay">
      <div class="modal">
        <h2>End User License Agreement</h2>
        <div class="eula-link">
          <button
            class="eula-link-btn"
            on:click={() => BrowserOpenURL('https://www.broadcom.com/company/legal/licensing')}>
            📄 View Broadcom Licensing Terms
          </button>
        </div>
        {#if currentEULA.content && currentEULA.content.trim()}
          <div class="eula-content">
            {currentEULA.content}
          </div>
        {:else}
          <p class="eula-notice">
            Please click the link above to review the full licensing terms before accepting.
          </p>
        {/if}
        <div class="modal-actions">
          <button class="cancel-btn" on:click={cancelEULA}>Decline</button>
          <button class="accept-btn" on:click={acceptEULAAndDownload}>Accept & Download</button>
        </div>
      </div>
    </div>
  {/if}

  {#if showBulkEULAModal}
    <div class="modal-overlay">
      <div class="modal bulk-eula-modal">
        <h2>Accept EULAs for All Products</h2>
        <p class="bulk-eula-description">
          By clicking "Accept All & Download", you agree to the End User License Agreements
          for all {bulkEULAProducts.length} products.
        </p>
        {#if bulkEULAProgress.includes('Processing') || bulkEULAProgress.includes('All EULAs accepted')}
          <div class="bulk-eula-progress">
            <div class="spinner"></div>
            <p class="loading-message">{bulkEULAProgress}</p>
          </div>
          <div class="modal-actions">
            <button class="cancel-btn" on:click={cancelBulkEULA} disabled>Cancel</button>
          </div>
        {:else}
          <div class="modal-actions">
            <button class="cancel-btn" on:click={cancelBulkEULA}>Cancel</button>
            <button class="accept-btn" on:click={acceptAllEULAsAndDownload}>Accept All & Download</button>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  {#if currentView === 'setup'}
    <div class="setup-container">
      <h2>Configure Broadcom API Token</h2>
      <p>Enter your Broadcom Support Portal API token to get started.</p>

      <div class="help-box">
        <h3>How to get your API token:</h3>
        <ol>
          <li>Visit <button class="inline-link" on:click={() => BrowserOpenURL('https://support.broadcom.com/group/ecx/tanzu-token')}>support.broadcom.com/group/ecx/tanzu-token</button></li>
          <li>Sign in to your Broadcom account</li>
          <li>Generate or copy your Tanzu API token</li>
          <li>Paste it below</li>
        </ol>
        <p class="note">Note: Your token will be securely stored locally in <code>~/.tanzu-downloader/config.json</code></p>
      </div>

      <div class="token-input">
        <input
          type="password"
          bind:value={apiToken}
          placeholder="Enter your API token"
          disabled={loading}
        />
        <button on:click={saveToken} disabled={loading}>
          {loading ? 'Saving...' : 'Save Token'}
        </button>
      </div>
    </div>
  {/if}

  {#if currentView === 'products'}
    <div class="content-container">
      <h2>Available Products</h2>
      <input
        type="text"
        class="search-input"
        bind:value={searchTerm}
        placeholder="Search products..."
      />
      {#if loading}
        <div class="loading">Loading products...</div>
      {:else}
        <div class="product-list">
          {#each filteredProducts as product}
            <div class="product-card" on:click={() => selectProduct(product)}>
              <h3>{product.name}</h3>
              <p class="product-slug">{product.slug}</p>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  {#if currentView === 'releases'}
    <div class="content-container">
      <button class="back-btn" on:click={backToProducts}>← Back to Products</button>
      <h2>{selectedProduct.name} - Releases</h2>
      {#if loading}
        <div class="loading">Loading releases...</div>
      {:else}
        <div class="release-list">
          {#each releases as release}
            <div class="release-card" on:click={() => selectRelease(release)}>
              <h3>Version {release.version}</h3>
              <p class="release-date">{release.release_date}</p>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}

  {#if currentView === 'files'}
    <div class="content-container">
      <button class="back-btn" on:click={backToReleases}>← Back to Releases</button>
      <h2>{selectedProduct.name} - Version {selectedRelease.version}</h2>
      {#if loading}
        <div class="loading">Loading files...</div>
      {:else}
        {#if categorizedFiles.mainFiles.length > 0}
          <div class="file-section">
            <h3 class="section-title">Main Downloads</h3>
            <p class="section-description">
              {#if selectedProduct.name.toLowerCase().includes('stemcell') || selectedProduct.slug.toLowerCase().includes('stemcell')}
                vSphere stemcells (prioritized)
              {:else if selectedProduct.name.toLowerCase().includes('foundation core') || selectedProduct.slug.toLowerCase().includes('ops-manager')}
                vSphere Ops Manager
              {:else}
                Tanzu tiles for deployment
              {/if}
            </p>
            <div class="file-list">
              {#each categorizedFiles.mainFiles as file}
                <div class="file-card main-file">
                  <div class="file-info">
                    <h3>{file.name}</h3>
                    <p class="file-details">
                      {#if file.name.toLowerCase().endsWith('.pivotal') || (file.aws_object_key && file.aws_object_key.toLowerCase().endsWith('.pivotal')) || (file.file_type && file.file_type.toLowerCase().includes('pivotal'))}
                        <span class="file-badge pivotal">Tile</span>
                      {:else if selectedProduct.name.toLowerCase().includes('stemcell') || selectedProduct.slug.toLowerCase().includes('stemcell')}
                        <span class="file-badge stemcell">Stemcell</span>
                      {:else if file.name.toLowerCase().endsWith('.ova') || selectedProduct.name.toLowerCase().includes('foundation core') || selectedProduct.slug.toLowerCase().includes('ops-manager')}
                        <span class="file-badge ova">Ops Manager</span>
                      {/if}
                    </p>
                  </div>
                  <div class="file-actions">
                    {#if downloads[file.id]}
                      <div class="download-progress">
                        {#if downloads[file.id].complete}
                          <span class="complete">✓ Downloaded</span>
                        {:else}
                          <progress value={downloads[file.id].progress} max="100"></progress>
                          <span>{downloads[file.id].progress.toFixed(1)}%</span>
                          {#if downloads[file.id].status}
                            <span class="status-text">{downloads[file.id].status}</span>
                          {/if}
                        {/if}
                      </div>
                      {#if !downloads[file.id].complete}
                        <button class="cancel-btn" on:click={() => cancelDownload(file.id)}>
                          Cancel
                        </button>
                      {/if}
                    {:else}
                      <button class="download-btn" on:click={() => downloadFile(file)}>
                        Download
                      </button>
                    {/if}
                    <button class="copy-cmd-btn" on:click={() => copyOMCommand(file)} title="Copy OM CLI command">
                      Copy
                    </button>
                  </div>
                </div>
              {/each}
            </div>
          </div>
        {/if}

        {#if categorizedFiles.otherFiles.length > 0}
          <div class="file-section">
            <h3 class="section-title other-section">Other Downloads</h3>
            <p class="section-description">Additional files and resources</p>
            <div class="file-list">
              {#each categorizedFiles.otherFiles as file}
                <div class="file-card">
                  <div class="file-info">
                    <h3>{file.name}</h3>
                    <p class="file-details">
                      {#if file.name.toLowerCase().endsWith('.pivotal') || (file.aws_object_key && file.aws_object_key.toLowerCase().endsWith('.pivotal')) || (file.file_type && file.file_type.toLowerCase().includes('pivotal'))}
                        <span class="file-badge pivotal">Tile</span>
                      {:else if selectedProduct.name.toLowerCase().includes('stemcell') || selectedProduct.slug.toLowerCase().includes('stemcell')}
                        <span class="file-badge stemcell">Stemcell</span>
                      {:else if file.name.toLowerCase().endsWith('.ova') || selectedProduct.name.toLowerCase().includes('foundation core') || selectedProduct.slug.toLowerCase().includes('ops-manager')}
                        <span class="file-badge ova">Ops Manager</span>
                      {/if}
                      Type: {file.file_type}
                    </p>
                  </div>
                  <div class="file-actions">
                    {#if downloads[file.id]}
                      <div class="download-progress">
                        {#if downloads[file.id].complete}
                          <span class="complete">✓ Downloaded</span>
                        {:else}
                          <progress value={downloads[file.id].progress} max="100"></progress>
                          <span>{downloads[file.id].progress.toFixed(1)}%</span>
                          {#if downloads[file.id].status}
                            <span class="status-text">{downloads[file.id].status}</span>
                          {/if}
                        {/if}
                      </div>
                      {#if !downloads[file.id].complete}
                        <button class="cancel-btn" on:click={() => cancelDownload(file.id)}>
                          Cancel
                        </button>
                      {/if}
                    {:else}
                      <button class="download-btn" on:click={() => downloadFile(file)}>
                        Download
                      </button>
                    {/if}
                    <button class="copy-cmd-btn" on:click={() => copyOMCommand(file)} title="Copy OM CLI command">
                      Copy
                    </button>
                  </div>
                </div>
              {/each}
            </div>
          </div>
        {/if}
      {/if}
    </div>
  {/if}

  {#if currentView === 'downloads'}
    <div class="content-container">
      <button class="back-btn" on:click={() => currentView = 'products'}>← Back to Products</button>
      <h2>Downloads</h2>

      {#if Object.keys(downloads).length === 0 && downloadQueue.length === 0}
        <p class="no-downloads">No active or queued downloads</p>
      {/if}

      {#if Object.keys(downloads).length > 0}
        <div class="active-section">
          <h3>Active & Completed</h3>
          <div class="downloads-list">
            {#each Object.entries(downloads) as [fileId, download]}
            <div class="download-item" class:completed={download.complete}>
              <div class="download-info">
                <p class="download-details">
                  {#if download.productName}
                    <span class="product-name">{download.productName}</span>
                  {/if}
                  {#if download.version}
                    <span class="version">v{download.version}</span>
                  {/if}
                  <span class="file-size">
                    {#if download.fileSize}
                      {#if download.fileSize >= 1024 * 1024 * 1024}
                        Size: {(download.fileSize / 1024 / 1024 / 1024).toFixed(2)} GB
                      {:else}
                        Size: {(download.fileSize / 1024 / 1024).toFixed(2)} MB
                      {/if}
                    {:else}
                      Size: Calculating...
                    {/if}
                  </span>
                </p>
                <h3>{download.fileName || `File #${fileId}`}</h3>
              </div>
              <div class="download-status">
                {#if download.complete}
                  <span class="complete">✓ Downloaded</span>
                  {#if download.path}
                    <p class="download-path">{download.path}</p>
                  {/if}
                {:else}
                  <div class="progress-container">
                    <progress value={download.progress || 0} max="100"></progress>
                    <span class="progress-text">{(download.progress || 0).toFixed(1)}%</span>
                    {#if download.status}
                      <span class="status-text">{download.status}</span>
                    {/if}
                  </div>
                  <button class="cancel-btn" on:click={() => cancelDownload(parseInt(fileId))}>
                    Cancel
                  </button>
                {/if}
              </div>
            </div>
            {/each}
          </div>
        </div>
      {/if}

      {#if downloadQueue.length > 0}
        <div class="queue-section">
          <h3>Queued ({downloadQueue.length})</h3>
          <div class="downloads-list">
            {#each downloadQueue as queuedItem, index}
              <div class="download-item queued">
                <div class="download-info">
                  <p class="download-details">
                    {#if queuedItem.productName}
                      <span class="product-name">{queuedItem.productName}</span>
                    {/if}
                    {#if queuedItem.version}
                      <span class="version">v{queuedItem.version}</span>
                    {/if}
                    <span class="queue-position">Position: {index + 1}</span>
                  </p>
                  <h3>{queuedItem.file.name}</h3>
                </div>
                <div class="download-status">
                  <span class="queued-status">⏳ Waiting...</span>
                </div>
              </div>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  {/if}

  {#if currentView === 'settings'}
    <div class="content-container">
      <h2>Settings</h2>

      <div class="settings-section">
        <h3>Broadcom API Token</h3>
        <p class="settings-description">Your Broadcom Support Portal API token</p>
        <div class="setting-input">
          <input
            type="password"
            bind:value={tempApiToken}
            placeholder="Enter your API token"
            disabled={loading}
          />
        </div>
        <p class="settings-note">Get your token from <a href="https://support.broadcom.com" target="_blank">Broadcom Support Portal</a></p>
      </div>

      <div class="settings-section">
        <h3>Download Location</h3>
        <p class="settings-description">Choose where downloaded files will be saved</p>
        <div class="setting-input">
          <input
            type="text"
            bind:value={tempDownloadLocation}
            placeholder="e.g., ~/Downloads/Tanzu"
            disabled={loading}
          />
        </div>
        <p class="settings-note">Current location: <code>{downloadLocation}</code></p>
      </div>

      <div class="settings-section">
        <h3>HTTP Proxy</h3>
        <p class="settings-description">Proxy server for HTTP requests (optional)</p>
        <div class="setting-input">
          <input
            type="text"
            bind:value={tempHttpProxy}
            placeholder="e.g., http://proxy.example.com:8080"
            disabled={loading}
          />
        </div>
        <p class="settings-note">Leave empty to disable HTTP proxy</p>
      </div>

      <div class="settings-section">
        <h3>HTTPS Proxy</h3>
        <p class="settings-description">Proxy server for HTTPS requests (optional)</p>
        <div class="setting-input">
          <input
            type="text"
            bind:value={tempHttpsProxy}
            placeholder="e.g., https://proxy.example.com:8443"
            disabled={loading}
          />
        </div>
        <p class="settings-note">Leave empty to disable HTTPS proxy</p>
      </div>

      <div class="settings-section">
        <h3>Product Filter</h3>
        <div class="checkbox-setting">
          <label>
            <input
              type="checkbox"
              bind:checked={onlyTanzuPlatform}
            />
            <span>Only show Tanzu Platform downloads</span>
          </label>
          <p class="settings-description">
            When enabled, filters out Kubernetes variants and shows only Tanzu Platform versions of products
          </p>
        </div>
      </div>

      <div class="settings-actions">
        <button class="cancel-btn" on:click={cancelSettings} disabled={loading}>Cancel</button>
        <button on:click={saveSettings} disabled={loading}>
          {loading ? 'Saving...' : 'Save Settings'}
        </button>
      </div>
    </div>
  {/if}

  {#if currentView === 'planner'}
    <div class="content-container">
      <div class="planner-header">
        <h2>Download Planner</h2>
        <button class="back-btn" on:click={() => currentView = 'products'}>← Back to Products</button>
      </div>

      {#if plannerError}
        <div class="error">
          {plannerError}
          <button class="error-dismiss" on:click={() => plannerError = ''}>×</button>
        </div>
      {/if}

      {#if plannerStep === 1}
        <div class="planner-step">
          <h3>Step 1: Select Ops Manager Version</h3>
          <p class="step-description">Choose the Ops Manager version you want to deploy</p>

          {#if plannerLoading}
            <p>Loading Ops Manager versions...</p>
          {:else}
            <div class="release-grid">
              {#each opsManagerReleases as release}
                <button class="release-card" on:click={() => selectOpsManagerVersion(release)}>
                  <div class="release-version">v{release.version}</div>
                  <div class="release-date">{new Date(release.release_date).toLocaleDateString()}</div>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {/if}

      {#if plannerStep === 2}
        <div class="planner-step">
          <h3>Step 2: Select Elastic Runtime (TAS) Version</h3>
          <p class="step-description">
            Selected Ops Manager: <strong>v{selectedOpsManager.version}</strong>
            <button class="change-link" on:click={() => backToPlannerStep(1)}>Change</button>
          </p>
          <p class="step-description">Choose the Tanzu Application Service version</p>

          {#if plannerLoading}
            <div class="loading-container">
              <div class="spinner"></div>
              <p class="loading-message">{plannerLoadingMessage || 'Loading compatible Elastic Runtime versions...'}</p>
            </div>
          {:else if elasticRuntimeReleases.length === 0}
            <p>No compatible Elastic Runtime versions found</p>
          {:else}
            <div class="release-grid">
              {#each elasticRuntimeReleases as release}
                <button class="release-card" on:click={() => selectElasticRuntimeVersion(release)}>
                  <div class="release-version">v{release.version}</div>
                  <div class="release-date">{new Date(release.release_date).toLocaleDateString()}</div>
                </button>
              {/each}
            </div>
          {/if}
        </div>
      {/if}

      {#if plannerStep === 3}
        <div class="planner-step">
          <h3>Step 3: Select TAS Type</h3>
          <p class="step-description">
            Selected Ops Manager: <strong>v{selectedOpsManager.version}</strong>
            <button class="change-link" on:click={() => backToPlannerStep(1)}>Change</button>
          </p>
          <p class="step-description">
            Selected Elastic Runtime: <strong>v{selectedElasticRuntime.version}</strong>
            <button class="change-link" on:click={() => backToPlannerStep(2)}>Change</button>
          </p>
          <p class="step-description">Choose the TAS deployment type</p>

          <div class="tas-type-selection">
            <button class="tas-type-card" on:click={() => selectTASType('full')}>
              <h4>Full Elastic Runtime</h4>
              <p class="tas-type-description">
                Complete Tanzu Application Service with all features and full scalability.
                Recommended for production environments.
              </p>
            </button>
            <button class="tas-type-card" on:click={() => selectTASType('srt')}>
              <h4>Small Footprint Runtime (SRT)</h4>
              <p class="tas-type-description">
                Lightweight version of TAS with reduced resource requirements.
                Ideal for development, testing, or smaller deployments.
              </p>
            </button>
          </div>
        </div>
      {/if}

      {#if plannerStep === 4}
        <div class="planner-step">
          <h3>Step 4: Review & Download Recommended Products</h3>
          <div class="selection-summary">
            <p>Ops Manager: <strong>v{selectedOpsManager.version}</strong>
              <button class="change-link" on:click={() => backToPlannerStep(1)}>Change</button>
            </p>
            <p>Elastic Runtime: <strong>v{selectedElasticRuntime.version}</strong>
              <button class="change-link" on:click={() => backToPlannerStep(2)}>Change</button>
            </p>
            <p>TAS Type: <strong>{selectedTASType === 'srt' ? 'Small Footprint Runtime' : 'Full Elastic Runtime'}</strong>
              <button class="change-link" on:click={() => backToPlannerStep(3)}>Change</button>
            </p>
          </div>

          {#if plannerLoading}
            <div class="loading-container">
              <div class="spinner"></div>
              <p class="loading-message">{plannerLoadingMessage || 'Loading product recommendations...'}</p>
            </div>
          {:else if recommendedProducts.length === 0}
            <p>No product recommendations available</p>
          {:else}
            <div class="planner-actions">
              <button class="download-all-btn" on:click={downloadAllPlannerProducts}>
                ⬇️ Download All ({recommendedProducts.length} products)
              </button>
            </div>

            <div class="recommended-products">
              {#each recommendedProducts as product}
                <div class="recommended-product">
                  <div class="product-header">
                    <h4>{product.productName}</h4>
                    <span class="product-version">v{product.version}</span>
                  </div>
                  <div class="product-files">
                    {#if product.files && product.files.length > 0}
                      {#each product.files as file}
                        <div class="file-row">
                          <span class="file-name">{file.name}</span>
                          <button
                            class="download-file-btn"
                            class:clicked={clickedButtons.has(`${product.productSlug}-${product.releaseId}-${file.id}`)}
                            on:click={() => downloadPlannerProduct(product, file)}
                          >
                            {clickedButtons.has(`${product.productSlug}-${product.releaseId}-${file.id}`) ? '✓ Queued' : 'Download'}
                          </button>
                        </div>
                      {/each}
                    {:else}
                      <p class="no-files">No files available</p>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  {#if currentView === 'aimodels'}
    <AIModelPackager {downloadLocation} />
  {/if}

  <footer>
    <p>
      Community project - Not affiliated with or supported by Broadcom or VMware Tanzu
      <br />
      <button class="github-link" on:click={openGitHub}>View on GitHub</button>
    </p>
  </footer>

  {#if showToast}
    <div class="toast" class:show={showToast}>
      {toastMessage}
    </div>
  {/if}
</main>

<style>
  :global(html),
  :global(body) {
    margin: 0;
    padding: 0;
    height: 100%;
  }

  :global(body) {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, Cantarell, sans-serif;
  }

  :global(#app) {
    min-height: 100%;
  }

  main {
    padding: 2rem;
    max-width: 1400px;
    margin: 0 auto;
    min-height: 100vh;
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
  }

  header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 2rem;
    padding: 1.5rem;
    background: rgba(255, 255, 255, 0.95);
    border-radius: 12px;
    box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
  }

  h1 {
    color: #667eea;
    margin: 0;
    font-weight: 700;
    font-size: 2rem;
  }

  h2 {
    color: #4a5568;
    margin-bottom: 1.5rem;
    font-weight: 600;
  }

  .error {
    background-color: #fed7d7;
    color: #c53030;
    padding: 1rem;
    border-radius: 8px;
    margin-bottom: 1rem;
    border-left: 4px solid #fc8181;
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 1rem;
  }

  .error-dismiss {
    background: none;
    border: none;
    color: #c53030;
    font-size: 1.5rem;
    cursor: pointer;
    padding: 0;
    width: 24px;
    height: 24px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    line-height: 1;
  }

  .error-dismiss:hover {
    color: #9b2c2c;
  }

  .setup-container {
    max-width: 600px;
    margin: 4rem auto;
    text-align: center;
    background: rgba(255, 255, 255, 0.95);
    padding: 3rem;
    border-radius: 12px;
    box-shadow: 0 10px 25px rgba(0, 0, 0, 0.2);
  }

  .setup-container h2 {
    color: #667eea;
  }

  .setup-container p {
    color: #718096;
  }

  .help-box {
    background: linear-gradient(135deg, #f7fafc 0%, #edf2f7 100%);
    border: 2px solid #e2e8f0;
    border-radius: 8px;
    padding: 1.5rem;
    margin: 2rem 0;
    text-align: left;
  }

  .help-box h3 {
    color: #667eea;
    margin-top: 0;
    margin-bottom: 1rem;
    font-size: 1.1rem;
    font-weight: 600;
  }

  .help-box ol {
    margin: 0.5rem 0 1rem 1.5rem;
    padding: 0;
    color: #4a5568;
  }

  .help-box li {
    margin: 0.5rem 0;
    line-height: 1.6;
  }

  .help-box a {
    color: #667eea;
    text-decoration: none;
    font-weight: 600;
    transition: color 0.2s;
  }

  .help-box a:hover {
    color: #764ba2;
    text-decoration: underline;
  }

  .inline-link {
    background: none;
    border: none;
    color: #667eea;
    text-decoration: none;
    font-weight: 600;
    cursor: pointer;
    padding: 0;
    font-size: inherit;
    font-family: inherit;
    transition: color 0.2s;
  }

  .inline-link:hover {
    color: #764ba2;
    text-decoration: underline;
  }

  .help-box .note {
    margin: 1rem 0 0 0;
    font-size: 0.9rem;
    color: #718096;
    font-style: italic;
  }

  .help-box code {
    background: rgba(102, 126, 234, 0.1);
    padding: 0.2rem 0.4rem;
    border-radius: 4px;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 0.85rem;
    color: #667eea;
  }

  .token-input {
    display: flex;
    gap: 1rem;
    margin-top: 2rem;
  }

  .token-input input {
    flex: 1;
    padding: 0.875rem;
    border: 2px solid #e2e8f0;
    border-radius: 8px;
    font-size: 1rem;
    transition: border-color 0.2s;
  }

  .token-input input:focus {
    outline: none;
    border-color: #667eea;
  }

  button {
    padding: 0.875rem 1.75rem;
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    color: white;
    border: none;
    border-radius: 8px;
    cursor: pointer;
    font-size: 1rem;
    font-weight: 600;
    transition: transform 0.2s, box-shadow 0.2s;
  }

  button:hover:not(:disabled) {
    transform: translateY(-2px);
    box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
  }

  button:disabled {
    background: #cbd5e0;
    cursor: not-allowed;
    transform: none;
  }

  .header-title {
    display: flex;
    align-items: center;
    gap: 1rem;
  }

  .tanzu-logo {
    height: 40px;
    width: auto;
  }

  .header-buttons {
    display: flex;
    gap: 0.75rem;
  }

  .downloads-btn {
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
    padding: 0.625rem 1.25rem;
    font-size: 0.9rem;
  }

  .downloads-btn:hover {
    box-shadow: 0 4px 12px rgba(72, 187, 120, 0.4);
  }

  .settings-btn {
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    padding: 0.625rem 1.25rem;
    font-size: 0.9rem;
  }

  .settings-btn:hover {
    box-shadow: 0 4px 12px rgba(102, 126, 234, 0.4);
  }

  .change-token-btn {
    background: linear-gradient(135deg, #718096 0%, #4a5568 100%);
    padding: 0.625rem 1.25rem;
    font-size: 0.9rem;
  }

  .change-token-btn:hover {
    box-shadow: 0 4px 12px rgba(74, 85, 104, 0.3);
  }

  .content-container {
    background: rgba(255, 255, 255, 0.95);
    padding: 2rem;
    border-radius: 12px;
    box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
  }

  .search-input {
    width: 100%;
    padding: 0.875rem;
    margin-bottom: 1.5rem;
    border: 2px solid #e2e8f0;
    border-radius: 8px;
    font-size: 1rem;
    transition: border-color 0.2s;
  }

  .search-input:focus {
    outline: none;
    border-color: #667eea;
  }

  .back-btn {
    background: linear-gradient(135deg, #718096 0%, #4a5568 100%);
    margin-bottom: 1rem;
  }

  .back-btn:hover {
    box-shadow: 0 4px 12px rgba(74, 85, 104, 0.3);
  }

  .loading {
    text-align: center;
    padding: 3rem;
    color: #667eea;
    font-size: 1.2rem;
    font-weight: 500;
  }

  .product-list, .release-list, .file-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 1.25rem;
  }

  .product-card, .release-card {
    padding: 1.75rem;
    border: 2px solid transparent;
    border-radius: 12px;
    cursor: pointer;
    transition: all 0.3s ease;
    background: white;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
  }

  .product-card:hover, .release-card:hover {
    border-color: #667eea;
    box-shadow: 0 8px 20px rgba(102, 126, 234, 0.25);
    transform: translateY(-4px);
  }

  .product-card h3, .release-card h3 {
    margin: 0 0 0.5rem 0;
    color: #2d3748;
    font-weight: 600;
  }

  .product-slug, .release-date {
    margin: 0;
    color: #718096;
    font-size: 0.9rem;
  }

  .file-list {
    grid-template-columns: 1fr;
  }

  .file-card {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 1.75rem;
    border: 2px solid transparent;
    border-radius: 12px;
    background: white;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
    transition: all 0.3s ease;
  }

  .file-card:hover {
    border-color: #48bb78;
    box-shadow: 0 4px 12px rgba(72, 187, 120, 0.15);
  }

  .file-info h3 {
    margin: 0 0 0.5rem 0;
    color: #2d3748;
    font-weight: 600;
  }

  .file-details {
    margin: 0;
    color: #718096;
    font-size: 0.9rem;
    text-align: left;
  }

  .file-actions {
    display: flex;
    gap: 0.75rem;
    align-items: center;
  }

  .download-btn {
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
  }

  .download-btn:hover {
    box-shadow: 0 4px 12px rgba(72, 187, 120, 0.4);
  }

  .copy-cmd-btn {
    padding: 0.875rem 1.75rem;
    background: linear-gradient(135deg, #4299e1 0%, #3182ce 100%);
    color: white;
    border: none;
    border-radius: 8px;
    cursor: pointer;
    font-size: 1rem;
    font-weight: 600;
    transition: all 0.2s;
    white-space: nowrap;
  }

  .copy-cmd-btn:hover {
    box-shadow: 0 4px 12px rgba(66, 153, 225, 0.4);
    transform: translateY(-1px);
  }

  .copy-cmd-btn:active {
    transform: translateY(0);
  }

  .download-progress {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 0.5rem;
    min-width: 200px;
  }

  .download-progress progress {
    width: 200px;
    height: 28px;
    border-radius: 14px;
    overflow: hidden;
  }

  .download-progress progress::-webkit-progress-bar {
    background-color: #e2e8f0;
    border-radius: 14px;
  }

  .download-progress progress::-webkit-progress-value {
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
    border-radius: 14px;
  }

  .download-progress span {
    font-size: 0.9rem;
    color: #4a5568;
    font-weight: 500;
  }

  .complete {
    color: #38a169;
    font-weight: 600;
    font-size: 1rem;
  }

  .modal-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background-color: rgba(0, 0, 0, 0.75);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
    backdrop-filter: blur(4px);
  }

  .modal {
    background-color: white;
    border-radius: 16px;
    padding: 2.5rem;
    max-width: 800px;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
    box-shadow: 0 20px 40px rgba(0, 0, 0, 0.3);
  }

  .modal h2 {
    margin-top: 0;
    margin-bottom: 1.5rem;
    color: #667eea;
    font-weight: 600;
  }

  .eula-link {
    margin-bottom: 1rem;
    text-align: center;
  }

  .eula-link-btn {
    color: #667eea;
    background: none;
    border: none;
    font-weight: 500;
    font-size: 0.95rem;
    cursor: pointer;
    padding: 0.5rem 1rem;
    transition: all 0.2s;
    text-decoration: underline;
  }

  .eula-link-btn:hover {
    color: #5a67d8;
    background-color: rgba(102, 126, 234, 0.1);
    border-radius: 4px;
  }

  .eula-notice {
    text-align: center;
    padding: 2rem 1rem;
    margin-bottom: 1.5rem;
    color: #4a5568;
    font-size: 0.95rem;
    line-height: 1.6;
  }

  .eula-content {
    flex: 1;
    overflow-y: auto;
    padding: 1.5rem;
    border: 2px solid #e2e8f0;
    border-radius: 8px;
    background-color: #f7fafc;
    margin-bottom: 1.5rem;
    white-space: pre-wrap;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 0.875rem;
    line-height: 1.6;
    color: #2d3748;
  }

  .modal-actions {
    display: flex;
    gap: 1rem;
    justify-content: flex-end;
  }

  .cancel-btn {
    background: linear-gradient(135deg, #718096 0%, #4a5568 100%);
  }

  .cancel-btn:hover {
    box-shadow: 0 4px 12px rgba(74, 85, 104, 0.3);
  }

  .accept-btn {
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
  }

  .accept-btn:hover {
    box-shadow: 0 4px 12px rgba(72, 187, 120, 0.4);
  }

  .bulk-eula-modal {
    max-width: 600px;
  }

  .bulk-eula-description {
    margin-bottom: 2rem;
    color: #4a5568;
    line-height: 1.6;
  }

  .bulk-eula-progress {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 1rem;
    padding: 2rem;
    background: #f7fafc;
    border-radius: 8px;
    margin-bottom: 2rem;
  }

  .settings-section {
    margin-bottom: 2rem;
  }

  .settings-section h3 {
    color: #2d3748;
    margin-bottom: 0.5rem;
    font-weight: 600;
  }

  .settings-description {
    color: #718096;
    margin-bottom: 1rem;
    font-size: 0.95rem;
  }

  .setting-input {
    margin-bottom: 0.75rem;
  }

  .setting-input input {
    width: 100%;
    padding: 0.875rem;
    border: 2px solid #e2e8f0;
    border-radius: 8px;
    font-size: 1rem;
    transition: border-color 0.2s;
  }

  .setting-input input:focus {
    outline: none;
    border-color: #667eea;
  }

  .settings-note {
    color: #718096;
    font-size: 0.9rem;
    font-style: italic;
  }

  .settings-note code {
    background: rgba(102, 126, 234, 0.1);
    padding: 0.2rem 0.4rem;
    border-radius: 4px;
    font-family: 'Consolas', 'Monaco', monospace;
    font-size: 0.85rem;
    color: #667eea;
  }

  .checkbox-setting {
    margin-bottom: 1rem;
  }

  .checkbox-setting label {
    display: flex;
    align-items: center;
    cursor: pointer;
    margin-bottom: 0.5rem;
  }

  .checkbox-setting input[type="checkbox"] {
    width: 20px;
    height: 20px;
    margin-right: 0.75rem;
    cursor: pointer;
    accent-color: #667eea;
  }

  .checkbox-setting span {
    font-size: 1rem;
    color: #2d3748;
    font-weight: 500;
  }

  .checkbox-setting .settings-description {
    margin-left: 2rem;
    margin-top: 0.25rem;
  }

  .settings-actions {
    display: flex;
    gap: 1rem;
    justify-content: flex-end;
    margin-top: 2rem;
    padding-top: 2rem;
    border-top: 2px solid #e2e8f0;
  }

  .file-section {
    margin-bottom: 3rem;
  }

  .section-title {
    color: #2d3748;
    font-size: 1.3rem;
    font-weight: 600;
    margin-bottom: 0.5rem;
  }

  .section-title.other-section {
    color: #718096;
    font-size: 1.1rem;
  }

  .section-description {
    color: #718096;
    margin-bottom: 1.25rem;
    font-size: 0.95rem;
  }

  .main-file {
    border: 2px solid #667eea;
    background: linear-gradient(135deg, rgba(102, 126, 234, 0.05) 0%, rgba(255, 255, 255, 1) 100%);
  }

  .main-file:hover {
    border-color: #764ba2;
    box-shadow: 0 8px 24px rgba(102, 126, 234, 0.3);
  }

  .file-badge {
    display: inline-block;
    padding: 0.25rem 0.75rem;
    border-radius: 12px;
    font-size: 0.8rem;
    font-weight: 600;
    margin-right: 0.75rem;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  .file-badge.pivotal {
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    color: white;
  }

  .file-badge.stemcell {
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
    color: white;
  }

  .file-badge.ova {
    background: linear-gradient(135deg, #ed8936 0%, #dd6b20 100%);
    color: white;
  }

  .status-text {
    display: block;
    font-size: 0.85rem;
    color: #667eea;
    margin-top: 0.25rem;
    font-style: italic;
  }
  /* Active Downloads Page */
  .queue-section {
    margin-bottom: 2rem;
  }

  .queue-section h3,
  .active-section h3 {
    color: #2d3748;
    margin-bottom: 1rem;
    font-size: 1.2rem;
  }

  .queue-position {
    color: #ed8936;
    font-weight: 600;
  }

  .queued-status {
    color: #ed8936;
    font-weight: 600;
    font-size: 1rem;
  }

  .no-downloads {
    text-align: center;
    color: #718096;
    padding: 3rem;
    font-size: 1.1rem;
  }

  .downloads-list {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .download-item {
    background: white;
    border-radius: 12px;
    padding: 1.5rem;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.1);
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    transition: all 0.2s;
  }

  .download-item.completed {
    background: linear-gradient(135deg, rgba(72, 187, 120, 0.05) 0%, rgba(255, 255, 255, 1) 100%);
    border-left: 4px solid #48bb78;
  }

  .download-item.queued {
    background: linear-gradient(135deg, rgba(237, 137, 54, 0.05) 0%, rgba(255, 255, 255, 1) 100%);
    border-left: 4px solid #ed8936;
  }

  .download-info {
    text-align: left;
  }

  .download-info h3 {
    margin: 0.5rem 0 0 0;
    color: #2d3748;
    font-size: 1.1rem;
  }

  .download-details {
    display: flex;
    gap: 1rem;
    margin: 0;
    font-size: 0.9rem;
    color: #718096;
    text-align: left;
  }

  .product-name {
    font-weight: 600;
    color: #667eea;
  }

  .version {
    color: #718096;
  }

  .download-status {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 1rem;
    min-width: 250px;
  }

  .progress-container {
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 0.5rem;
  }

  .download-status progress {
    width: 250px;
    height: 28px;
  }

  .progress-text {
    font-size: 0.9rem;
    font-weight: 600;
    color: #667eea;
  }

  .download-path {
    font-size: 0.85rem;
    color: #718096;
    margin: 0.5rem 0 0 0;
    font-style: italic;
  }

  footer {
    text-align: center;
    padding: 1.5rem 1rem;
    margin-top: 2rem;
    border-top: 1px solid rgba(255, 255, 255, 0.2);
  }

  footer p {
    margin: 0;
    font-size: 0.85rem;
    color: rgba(255, 255, 255, 0.8);
    font-style: italic;
  }

  .github-link {
    background: none;
    border: none;
    color: rgba(255, 255, 255, 0.9);
    text-decoration: underline;
    font-size: 0.85rem;
    font-style: italic;
    cursor: pointer;
    padding: 0;
    margin-top: 0.25rem;
    transition: color 0.2s ease;
  }

  .github-link:hover {
    color: #ffffff;
  }

  /* Download Planner Styles */
  .planner-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 2rem;
  }

  .planner-step {
    background: white;
    border-radius: 12px;
    padding: 2rem;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  }

  .planner-step h3 {
    color: #2d3748;
    margin-bottom: 1rem;
  }

  .step-description {
    color: #718096;
    margin-bottom: 1.5rem;
  }

  .release-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 1rem;
    margin-top: 1rem;
  }

  .release-card {
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    color: white;
    border: none;
    border-radius: 8px;
    padding: 1.5rem 1rem;
    cursor: pointer;
    transition: all 0.2s;
    text-align: center;
  }

  .release-card:hover {
    transform: translateY(-2px);
    box-shadow: 0 6px 16px rgba(102, 126, 234, 0.4);
  }

  .release-card h3 {
    color: white;
  }

  .release-version {
    font-size: 1.2rem;
    font-weight: 600;
    margin-bottom: 0.5rem;
    color: white;
  }

  .release-date {
    font-size: 0.85rem;
    color: rgba(255, 255, 255, 0.7);
    font-weight: 400;
  }

  .tas-type-selection {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 1.5rem;
    margin-top: 1.5rem;
  }

  .tas-type-card {
    background: white;
    border: 2px solid #e2e8f0;
    border-radius: 12px;
    padding: 2rem;
    cursor: pointer;
    transition: all 0.3s ease;
    text-align: left;
  }

  .tas-type-card:hover {
    border-color: #667eea;
    box-shadow: 0 8px 20px rgba(102, 126, 234, 0.25);
    transform: translateY(-4px);
  }

  .tas-type-card h4 {
    margin: 0 0 1rem 0;
    color: #667eea;
    font-weight: 600;
    font-size: 1.2rem;
  }

  .tas-type-description {
    margin: 0;
    color: #4a5568;
    line-height: 1.6;
    font-size: 0.95rem;
  }

  .selection-summary {
    background: #f7fafc;
    border-radius: 8px;
    padding: 1rem;
    margin-bottom: 2rem;
  }

  .selection-summary p {
    margin: 0.5rem 0;
    color: #2d3748;
  }

  .change-link {
    background: none;
    border: none;
    color: #667eea;
    cursor: pointer;
    text-decoration: underline;
    font-size: 0.9rem;
    margin-left: 0.5rem;
    padding: 0;
  }

  .change-link:hover {
    color: #764ba2;
  }

  .planner-actions {
    margin-bottom: 2rem;
    display: flex;
    justify-content: center;
  }

  .download-all-btn {
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
    color: white;
    border: none;
    padding: 1rem 2rem;
    font-size: 1.1rem;
    font-weight: 600;
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s;
    box-shadow: 0 4px 12px rgba(72, 187, 120, 0.3);
  }

  .download-all-btn:hover {
    transform: translateY(-2px);
    box-shadow: 0 6px 16px rgba(72, 187, 120, 0.4);
  }

  .recommended-products {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .recommended-product {
    background: #f7fafc;
    border-radius: 8px;
    padding: 1.5rem;
    border-left: 4px solid #667eea;
  }

  .product-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 1rem;
  }

  .product-header h4 {
    margin: 0;
    color: #2d3748;
    font-size: 1.1rem;
  }

  .product-version {
    background: #667eea;
    color: white;
    padding: 0.25rem 0.75rem;
    border-radius: 12px;
    font-size: 0.9rem;
    font-weight: 600;
  }

  .product-files {
    margin-top: 1rem;
  }

  .file-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.75rem;
    background: white;
    border-radius: 6px;
    margin-bottom: 0.5rem;
  }

  .file-name {
    color: #2d3748;
    font-size: 0.95rem;
    flex: 1;
  }

  .download-file-btn {
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    color: white;
    border: none;
    padding: 0.5rem 1rem;
    border-radius: 6px;
    cursor: pointer;
    font-weight: 600;
    transition: all 0.2s;
  }

  .download-file-btn:hover {
    transform: translateY(-1px);
    box-shadow: 0 4px 8px rgba(102, 126, 234, 0.3);
  }

  .download-file-btn.clicked {
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
    transform: scale(0.95);
    box-shadow: 0 2px 4px rgba(72, 187, 120, 0.3);
  }

  .download-file-btn.clicked:hover {
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
    transform: scale(0.95);
  }

  .no-files {
    color: #a0aec0;
    font-style: italic;
    margin: 0;
  }

  .planner-btn {
    background: linear-gradient(135deg, #f6ad55 0%, #ed8936 100%);
    padding: 0.625rem 1.25rem;
    font-size: 0.9rem;
  }

  .planner-btn:hover {
    box-shadow: 0 4px 12px rgba(246, 173, 85, 0.4);
  }

  .aimodels-btn {
    background: linear-gradient(135deg, #9f7aea 0%, #805ad5 100%);
    padding: 0.625rem 1.25rem;
    font-size: 0.9rem;
  }

  .aimodels-btn:hover {
    box-shadow: 0 4px 12px rgba(159, 122, 234, 0.4);
  }

  .loading-container {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 3rem 2rem;
    gap: 1rem;
  }

  .spinner {
    width: 50px;
    height: 50px;
    border: 4px solid rgba(102, 126, 234, 0.2);
    border-top-color: #667eea;
    border-radius: 50%;
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  .loading-message {
    color: #2d3748;
    font-size: 1.1rem;
    margin: 0;
    font-weight: 500;
  }

  /* Toast Notification */
  .toast {
    position: fixed;
    bottom: 2rem;
    right: 2rem;
    background: linear-gradient(135deg, #48bb78 0%, #38a169 100%);
    color: white;
    padding: 1rem 1.5rem;
    border-radius: 8px;
    box-shadow: 0 4px 12px rgba(72, 187, 120, 0.4);
    font-weight: 500;
    z-index: 10000;
    opacity: 0;
    transform: translateY(20px);
    transition: all 0.3s ease;
    pointer-events: none;
  }

  .toast.show {
    opacity: 1;
    transform: translateY(0);
  }
</style>
