SRC_PATH="github.com/emef/djv_ads"

echo "deploying to $DJV_HOST"
echo "using TJ api key $TJ_API_KEY"
echo "using TJ username $TJ_USERNAME"
echo "using TJ password $TJ_PASSWORD"

echo "ensuring djv_ads user exists"
ssh $DJV_HOST "sudo useradd djv_ads -m &>/dev/null"

echo "stopping server for deploy"
ssh $DJV_HOST "sudo systemctl stop djv_ads"

echo "uploading and installing djv_ads_controller"
ssh $DJV_HOST "mkdir -p go/src/github.com/emef/djv_ads"
rsync -az $GOPATH/src/github.com/emef/djv_ads $DJV_HOST:go/src/github.com/emef/
ssh $DJV_HOST "GOPATH=\$HOME/go go get -t -d github.com/emef/djv_ads/..."
ssh $DJV_HOST "GOPATH=\$HOME/go go install github.com/emef/djv_ads/djv_ads_controller"
ssh $DJV_HOST "sudo cp go/bin/djv_ads_controller /usr/local/bin"
ssh $DJV_HOST "sudo chmod a+x /usr/local/bin/djv_ads_controller"
ssh $DJV_HOST "sudo mkdir -p /opt/djv_ads/templates"
ssh $DJV_HOST "sudo cp -r go/src/github.com/emef/djv_ads/templates /opt/djv_ads/"
ssh $DJV_HOST "sudo chown -R djv_ads /opt/djv_ads"

echo "installing systemd config and starting server"
scp $GOPATH/src/github.com/emef/djv_ads/deploy/djv_ads.service $DJV_HOST: 2>/dev/null
ssh $DJV_HOST "sudo mv djv_ads.service /lib/systemd/system/"
ssh $DJV_HOST "sudo sed -i 's/\\\$TJ_API_KEY/$TJ_API_KEY/' /lib/systemd/system/djv_ads.service"
ssh $DJV_HOST "sudo sed -i 's/\\\$TJ_USERNAME/$TJ_USERNAME/' /lib/systemd/system/djv_ads.service"
ssh $DJV_HOST "sudo sed -i 's/\\\$TJ_PASSWORD/$TJ_PASSWORD/' /lib/systemd/system/djv_ads.service"
ssh $DJV_HOST "sudo chmod 755 /lib/systemd/system/djv_ads.service"
ssh $DJV_HOST "sudo systemctl daemon-reload"
ssh $DJV_HOST "sudo systemctl enable djv_ads.service"
ssh $DJV_HOST "sudo systemctl start djv_ads"

echo "installing nginx config and restarting it"
scp $GOPATH/src/github.com/emef/djv_ads/deploy/nginx.conf $DJV_HOST: 2>/dev/null
ssh $DJV_HOST "sudo mv nginx.conf /etc/nginx/sites-available/djv_ads"
ssh $DJV_HOST "sudo ln -s /etc/nginx/sites-available/djv_ads /etc/nginx/sites-enabled/ 2>/dev/null"
ssh $DJV_HOST "sudo service nginx restart"
