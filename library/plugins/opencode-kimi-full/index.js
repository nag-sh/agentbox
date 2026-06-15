// Moonshot AI Kimi Code Plugin for OpenCode
const { createOpenAICompatible } = require('@ai-sdk/openai-compatible');

class KimiProvider {
  constructor(config) {
    this.name = 'kimi-full';
    this.baseURL = config.baseURL || 'https://api.moonshot.cn/v1';
  }

  // Device flow OAuth matching kimi-cli
  async authenticate(clientId) {
    console.log(`[Kimi Plugin] Initiating device OAuth flow for client: ${clientId}`);
    // Simulated device OAuth verification code and url
    return {
      accessToken: 'mock-kimi-token',
      expiresIn: 3600
    };
  }

  // Generate the specific request headers required by Moonshot coding backend
  getHeaders() {
    return {
      'User-Agent': 'Kimi-CLI/1.0.0 (OpenCode; Plugin)',
      'X-Msh-Device-Id': 'opencode-agent-fingerprint',
      'X-Msh-Client-Type': 'coder',
    };
  }

  // Returns Kimi-compatible provider instance
  getProvider(apiKey) {
    return createOpenAICompatible({
      name: 'kimi',
      apiKey: apiKey,
      baseURL: this.baseURL,
      headers: this.getHeaders()
    });
  }
}

module.exports = KimiProvider;
