<script>
  import { onMount } from 'svelte';
  import { DownloadOllamaModel, DownloadVLLMModel, CancelModelDownload, SetDownloadLocation } from '../../wailsjs/go/main/AIModelService.js';
  import { GetDownloadLocation } from '../../wailsjs/go/main/BroadcomService.js';
  import { EventsOn } from '../../wailsjs/runtime/runtime.js';
  import { BrowserOpenURL } from '../../wailsjs/runtime/runtime.js';

  export let downloadLocation = '';

  let modelType = 'vllm'; // 'ollama' or 'vllm'
  let huggingfaceURL = '';
  let modelName = '';
  let downloading = false;
  let error = '';
  let activeDownloads = {};
  let completedDownloads = [];

  onMount(async () => {
    // Load download location
    try {
      downloadLocation = await GetDownloadLocation();
      await SetDownloadLocation(downloadLocation);
    } catch (e) {
      console.error('Failed to get download location:', e);
    }

    // Listen for download events
    EventsOn('ai-model-status', (data) => {
      const currentDownload = activeDownloads[data.modelName] || {};
      activeDownloads[data.modelName] = {
        status: data.status,
        // Keep current progress if data.progress is -1, otherwise use new value
        progress: data.progress === -1 ? (currentDownload.progress || 0) : (data.progress || 0),
      };
      activeDownloads = activeDownloads; // Trigger reactivity
    });

    EventsOn('ai-model-complete', (data) => {
      completedDownloads = [...completedDownloads, {
        modelName: data.modelName,
        path: data.path,
        timestamp: new Date().toLocaleString(),
      }];
      delete activeDownloads[data.modelName];
      activeDownloads = activeDownloads;
      downloading = false;
    });

    EventsOn('ai-model-cancelled', (data) => {
      delete activeDownloads[data.modelName];
      activeDownloads = activeDownloads;
      downloading = false;
    });
  });

  async function startDownload() {
    if (!huggingfaceURL || !modelName) {
      error = 'Please provide both HuggingFace URL and model name';
      return;
    }

    if (!downloadLocation) {
      error = 'Download location not set';
      return;
    }

    error = '';
    downloading = true;

    try {
      if (modelType === 'ollama') {
        await DownloadOllamaModel(huggingfaceURL, modelName);
      } else {
        await DownloadVLLMModel(huggingfaceURL, modelName);
      }
    } catch (e) {
      // Don't show error if it's a cancellation
      const errMsg = e.toString().toLowerCase();
      if (!errMsg.includes('cancel')) {
        error = 'Download failed: ' + e.toString();
      }
      downloading = false;
    }
  }

  async function cancelDownload(modelName) {
    try {
      await CancelModelDownload(modelName);
    } catch (e) {
      error = 'Failed to cancel download: ' + e.toString();
    }
  }
</script>

<div class="ai-model-packager">
  <div class="header">
    <h2>AI Model Packager</h2>
    <p class="subtitle">Download and package AI models for Tanzu Platform AI Services (GenAI)</p>
  </div>

  {#if error}
    <div class="error">
      {error}
      <button class="error-dismiss" on:click={() => error = ''}>×</button>
    </div>
  {/if}

  <div class="form-section model-type-section">
    <h3>Model Type</h3>
    <div class="radio-group">
      <label class="radio-option">
        <input type="radio" bind:group={modelType} value="vllm" />
        <div class="radio-content">
          <strong>vLLM</strong>
          <span>Downloads safetensors, JSON, and jinja files, then packages as tar.gz</span>
        </div>
      </label>
      <label class="radio-option">
        <input type="radio" bind:group={modelType} value="ollama" />
        <div class="radio-content">
          <strong>Ollama</strong>
          <span>Downloads GGUF files and concatenates to a single file</span>
        </div>
      </label>
    </div>
  </div>

  <div class="download-form">
    <div class="form-section">
      <h3>Model Details</h3>
      <div class="form-group">
        <label for="huggingface-url">HuggingFace URL</label>
        <input
          id="huggingface-url"
          type="text"
          bind:value={huggingfaceURL}
          placeholder={modelType === 'ollama'
            ? 'https://huggingface.co/unsloth/Llama-3.3-70B-Instruct-GGUF/tree/main/UD-Q6_K_XL'
            : 'https://huggingface.co/openai/gpt-oss-120b'}
          disabled={downloading}
        />
        <small>
          {#if modelType === 'ollama'}
            Enter the full path to the directory containing GGUF files
          {:else}
            Enter the repository URL (will download from root level)
          {/if}
        </small>
      </div>

      <div class="form-group">
        <label for="model-name">Model Name</label>
        <input
          id="model-name"
          type="text"
          bind:value={modelName}
          placeholder="my-model-name"
          disabled={downloading}
        />
        <small>
          {#if modelType === 'ollama'}
            Name for the downloaded model directory
          {:else}
            Name for the output tar.gz file (will be saved as modelname.tar.gz)
          {/if}
        </small>
      </div>

      <div class="form-group">
        <label>Download Location</label>
        <div class="download-location">{downloadLocation || 'Not set'}</div>
        <small>Models will be downloaded to this location (configured in Settings)</small>
      </div>

      <button
        class="download-btn"
        on:click={startDownload}
        disabled={downloading || !huggingfaceURL || !modelName}>
        {downloading ? 'Downloading...' : 'Download & Package Model'}
      </button>
    </div>
  </div>

  {#if Object.keys(activeDownloads).length > 0}
    <div class="active-downloads">
      <h3>Active Downloads</h3>
      {#each Object.entries(activeDownloads) as [name, download]}
        <div class="download-item">
          <div class="download-header">
            <strong>{name}</strong>
            <button class="cancel-btn-small" on:click={() => cancelDownload(name)}>Cancel</button>
          </div>
          <div class="download-status">{download.status}</div>
          {#if download.progress > 0}
            <div class="progress-bar">
              <div class="progress-fill" style="width: {download.progress}%"></div>
            </div>
            <div class="progress-text">{download.progress}%</div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}

  {#if completedDownloads.length > 0}
    <div class="completed-downloads">
      <h3>Completed Downloads</h3>
      {#each completedDownloads as download}
        <div class="download-item completed">
          <div class="download-header">
            <strong>{download.modelName}</strong>
            <span class="timestamp">{download.timestamp}</span>
          </div>
          <button class="download-path-link" on:click={() => BrowserOpenURL('file://' + download.path)}>
            📁 {download.path}
          </button>
        </div>
      {/each}
    </div>
  {/if}

  <div class="info-section">
    <h3>Prerequisites</h3>
    <div class="prerequisites-grid">
      <div class="prerequisite-box">
        <h4>macOS</h4>
        <p>Install HuggingFace CLI via Homebrew:</p>
        <code>brew install huggingface-cli</code>
        <p class="note">The CLI command is <code>hf</code></p>
        <p>Authenticate with HuggingFace:</p>
        <code>hf auth login</code>
      </div>
      <div class="prerequisite-box">
        <h4>Windows / Linux</h4>
        <p>Install HuggingFace CLI via pip:</p>
        <code>pip install huggingface-hub[cli]</code>
        <p class="note">The CLI command is <code>huggingface-cli</code></p>
        <p>Authenticate with HuggingFace:</p>
        <code>huggingface-cli auth login</code>
      </div>
    </div>
    <p class="disk-space-note">⚠️ Sufficient disk space required for model files (can be very large, 100GB+)</p>

    <h3>Model Type Guidelines</h3>
    <div class="guidelines">
      <div class="guideline">
        <h4>vLLM Models</h4>
        <p>For standard transformer models served with vLLM:</p>
        <ul>
          <li>Downloads .safetensors, .json, and .jinja files</li>
          <li>Automatically packages as tar.gz with files at root level</li>
        </ul>
        <p>Example:</p>
        <code>https://huggingface.co/openai/gpt-oss-120b</code>
      </div>
      <div class="guideline">
        <h4>Ollama Models (GGUF)</h4>
        <p>For quantized models in GGUF format:</p>
        <ul>
          <li>Downloads .gguf files from huggingface</li>
          <li>Concatenates to a single GGUF</li>
        </ul>
        <p>Example:</p>
        <code>https://huggingface.co/unsloth/Llama-3.3-70B-Instruct-GGUF/tree/main/UD-Q6_K_XL</code>
      </div>
    </div>
  </div>
</div>

<style>
  .ai-model-packager {
    padding: 2rem;
    max-width: 1200px;
    margin: 0 auto;
    background: rgba(255, 255, 255, 0.95);
    border-radius: 12px;
    box-shadow: 0 10px 25px rgba(0, 0, 0, 0.2);
  }

  .header {
    margin-bottom: 2rem;
    text-align: left;
  }

  .header h2 {
    margin: 0;
    color: #667eea;
    font-size: 2rem;
  }

  .subtitle {
    margin: 0.5rem 0 0 0;
    color: #4a5568;
    font-size: 1rem;
  }

  .model-type-section {
    background-color: #f7fafc;
    border-radius: 12px;
    padding: 2rem;
    margin-bottom: 2rem;
  }

  .model-type-section h3 {
    text-align: left;
    margin: 0 0 1rem 0;
    color: #2d3748;
    font-size: 1.25rem;
  }

  .error {
    background-color: #fed7d7;
    color: #c53030;
    padding: 1rem;
    border-radius: 8px;
    margin-bottom: 1.5rem;
    position: relative;
    display: flex;
    justify-content: space-between;
    align-items: center;
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
    margin-left: 1rem;
  }

  .error-dismiss:hover {
    color: #9b2c2c;
    transform: none;
    box-shadow: none;
  }

  .download-form {
    background-color: #f7fafc;
    border-radius: 12px;
    padding: 2rem;
    margin-bottom: 2rem;
  }

  .form-section {
    margin-bottom: 2rem;
  }

  .form-section:last-child {
    margin-bottom: 0;
  }

  .form-section h3 {
    margin: 0 0 1rem 0;
    color: #2d3748;
    font-size: 1.25rem;
  }

  .radio-group {
    display: flex;
    flex-direction: column;
    gap: 1rem;
  }

  .radio-option {
    display: flex;
    align-items: start;
    padding: 1rem;
    background-color: white;
    border: 2px solid #e2e8f0;
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.2s;
  }

  .radio-option:hover {
    border-color: #667eea;
    background-color: #edf2f7;
  }

  .radio-option input[type="radio"] {
    margin-right: 1rem;
    margin-top: 0.25rem;
    cursor: pointer;
  }

  .radio-content {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    text-align: left;
  }

  .radio-content strong {
    color: #2d3748;
    text-align: left;
  }

  .radio-content span {
    color: #718096;
    font-size: 0.875rem;
    text-align: left;
  }

  .form-group {
    margin-bottom: 1.5rem;
  }

  .form-group label {
    display: block;
    margin-bottom: 0.5rem;
    color: #2d3748;
    font-weight: 500;
  }

  .form-group input {
    width: 100%;
    padding: 0.75rem;
    border: 2px solid #e2e8f0;
    border-radius: 8px;
    font-size: 1rem;
    transition: border-color 0.2s;
  }

  .form-group input:focus {
    outline: none;
    border-color: #667eea;
  }

  .form-group input:disabled {
    background-color: #edf2f7;
    cursor: not-allowed;
  }

  .form-group small {
    display: block;
    margin-top: 0.5rem;
    color: #718096;
    font-size: 0.875rem;
  }

  .download-location {
    padding: 0.75rem;
    background-color: #edf2f7;
    border-radius: 8px;
    color: #2d3748;
    font-family: monospace;
  }

  .download-btn {
    width: 100%;
    padding: 1rem;
    background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
    color: white;
    border: none;
    border-radius: 8px;
    font-size: 1rem;
    font-weight: 600;
    cursor: pointer;
    transition: transform 0.2s;
  }

  .download-btn:hover:not(:disabled) {
    transform: translateY(-2px);
  }

  .download-btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  .active-downloads,
  .completed-downloads {
    background-color: #f7fafc;
    border-radius: 12px;
    padding: 1.5rem;
    margin-bottom: 2rem;
  }

  .active-downloads h3,
  .completed-downloads h3 {
    margin: 0 0 1rem 0;
    color: #2d3748;
  }

  .download-item {
    background-color: white;
    border: 2px solid #e2e8f0;
    border-radius: 8px;
    padding: 1rem;
    margin-bottom: 1rem;
  }

  .download-item:last-child {
    margin-bottom: 0;
  }

  .download-item.completed {
    border-color: #48bb78;
  }

  .download-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.5rem;
  }

  .download-status {
    color: #718096;
    font-size: 0.875rem;
    margin-bottom: 0.5rem;
  }

  .download-path {
    color: #718096;
    font-size: 0.875rem;
    font-family: monospace;
  }

  .download-path-link {
    background: none;
    border: none;
    color: #667eea;
    font-size: 0.875rem;
    font-family: monospace;
    cursor: pointer;
    padding: 0.5rem;
    margin: 0;
    text-align: left;
    width: 100%;
    text-decoration: underline;
  }

  .download-path-link:hover {
    color: #764ba2;
    background-color: #f7fafc;
    border-radius: 4px;
  }

  .timestamp {
    color: #718096;
    font-size: 0.875rem;
  }

  .cancel-btn-small {
    background-color: #fc8181;
    color: white;
    border: none;
    padding: 0.5rem 1rem;
    border-radius: 6px;
    cursor: pointer;
    font-size: 0.875rem;
  }

  .cancel-btn-small:hover {
    background-color: #f56565;
  }

  .progress-bar {
    width: 100%;
    height: 8px;
    background-color: #e2e8f0;
    border-radius: 4px;
    overflow: hidden;
    margin-bottom: 0.5rem;
  }

  .progress-fill {
    height: 100%;
    background: linear-gradient(90deg, #667eea 0%, #764ba2 100%);
    transition: width 0.3s;
  }

  .progress-text {
    text-align: right;
    color: #718096;
    font-size: 0.875rem;
  }

  .info-section {
    background-color: #f7fafc;
    border-radius: 12px;
    padding: 2rem;
  }

  .info-section h3 {
    margin: 0 0 1.5rem 0;
    color: #2d3748;
    font-size: 1.25rem;
  }

  .info-section ul {
    margin: 0 0 1.5rem 0;
    padding-left: 1.5rem;
  }

  .info-section li {
    margin-bottom: 0.5rem;
    color: #4a5568;
  }

  .info-section code {
    background-color: #2d3748;
    color: #68d391;
    padding: 0.375rem 0.625rem;
    border-radius: 6px;
    font-size: 0.875rem;
    font-family: 'Monaco', 'Menlo', 'Courier New', monospace;
    display: inline-block;
    margin: 0.25rem 0;
  }

  .prerequisites-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1.5rem;
    margin-bottom: 1.5rem;
  }

  .prerequisite-box {
    background-color: white;
    border: 2px solid #e2e8f0;
    border-radius: 10px;
    padding: 1.5rem;
  }

  .prerequisite-box h4 {
    margin: 0 0 1rem 0;
    color: #667eea;
    font-size: 1.125rem;
  }

  .prerequisite-box p {
    margin: 0.75rem 0 0.5rem 0;
    color: #4a5568;
    font-size: 0.875rem;
    font-weight: 500;
  }

  .prerequisite-box p.note {
    margin: 0.5rem 0 0.75rem 0;
    color: #718096;
    font-size: 0.8125rem;
    font-style: italic;
  }

  .prerequisite-box code {
    display: block;
    margin: 0.5rem 0;
    padding: 0.625rem 0.875rem;
    background-color: #2d3748;
    color: #68d391;
    border-radius: 6px;
    font-size: 0.875rem;
    font-family: 'Monaco', 'Menlo', 'Courier New', monospace;
  }

  .disk-space-note {
    background-color: #fef5e7;
    border-left: 4px solid #f6ad55;
    padding: 1rem;
    border-radius: 6px;
    color: #744210;
    font-size: 0.9375rem;
    margin: 1.5rem 0;
  }

  .guidelines {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1.5rem;
  }

  .guideline {
    background-color: white;
    padding: 1.5rem;
    border-radius: 8px;
    border: 2px solid #e2e8f0;
  }

  .guideline h4 {
    margin: 0 0 0.75rem 0;
    color: #667eea;
    font-size: 1.125rem;
  }

  .guideline p {
    margin: 0 0 0.75rem 0;
    color: #4a5568;
    font-size: 0.875rem;
    text-align: left;
  }

  .guideline ul {
    margin: 0;
    padding-left: 1.5rem;
    text-align: left;
  }

  .guideline li {
    font-size: 0.875rem;
    color: #4a5568;
    margin-bottom: 0.5rem;
    text-align: left;
  }

  .guideline code {
    display: block;
    margin: 0.5rem 0 0 0;
    padding: 0.625rem 0.875rem;
    background-color: #2d3748;
    color: #68d391;
    border-radius: 6px;
    font-size: 0.875rem;
    font-family: 'Monaco', 'Menlo', 'Courier New', monospace;
    word-break: break-all;
    text-align: left;
  }

  @media (max-width: 768px) {
    .prerequisites-grid,
    .guidelines {
      grid-template-columns: 1fr;
    }
  }
</style>
