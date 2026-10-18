document.getElementById('extensionVersion').textContent = `Version ${chrome.runtime.getManifest().version}.`
// Store builds have no "Allow User Scripts" toggle to point at.
document.getElementById('userScriptsGuide').hidden = !chrome.runtime.getManifest().permissions.includes('userScripts')
