import { mount } from 'svelte';
import App from './pages/App.svelte';
import './styles/app.css';
mount(App, { target: document.getElementById('app')! });
